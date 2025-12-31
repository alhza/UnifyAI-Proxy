package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/unifyai-proxy/unifyai-proxy/internal/account"
	"github.com/unifyai-proxy/unifyai-proxy/internal/auth"
	"github.com/unifyai-proxy/unifyai-proxy/internal/config"
	"github.com/unifyai-proxy/unifyai-proxy/internal/logging"
	"github.com/unifyai-proxy/unifyai-proxy/internal/metrics"
	"github.com/unifyai-proxy/unifyai-proxy/internal/provider"
	"github.com/unifyai-proxy/unifyai-proxy/internal/server"
	"github.com/unifyai-proxy/unifyai-proxy/internal/transformer"
)

// Version information (set by build flags)
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

func main() {
	// Parse command line flags
	var (
		configPath  = flag.String("config", "config.yaml", "Path to configuration file")
		showVersion = flag.Bool("version", false, "Show version information")
		loginCmd    = flag.String("login", "", "Login to a provider (claude/gemini)")
	)
	flag.Parse()

	// Show version if requested
	if *showVersion {
		fmt.Printf("UnifyAI Proxy %s\n", Version)
		fmt.Printf("  Git Commit: %s\n", GitCommit)
		fmt.Printf("  Build Time: %s\n", BuildTime)
		os.Exit(0)
	}

	// Load configuration with hot-reload support
	configWatcher, err := config.NewConfigWatcher(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	cfg := configWatcher.GetConfig()

	// Initialize logging
	logging.SetupWithLevel(cfg.Logging.Level)
	slog.Info("UnifyAI Proxy starting",
		"version", Version,
		"config", *configPath)

	// Start config watcher for hot-reload
	if err := configWatcher.Start(); err != nil {
		slog.Warn("Failed to start config watcher, hot-reload disabled", "error", err)
	} else {
		slog.Info("Config hot-reload enabled")
	}

	// Initialize token store with encryption configuration
	// In commercial mode, encryption is mandatory for security
	tokenStore, err := auth.NewFileStoreWithConfig(auth.FileStoreConfig{
		BaseDir:           cfg.AuthDir,
		EncryptionKey:     []byte(cfg.AuthEncryptionKey),
		RequireEncryption: cfg.CommercialMode, // Mandatory in commercial mode
		WarnUnencrypted:   true,                // Always warn if unencrypted
	})
	if err != nil {
		if err == auth.ErrEncryptionRequired {
			slog.Error("Token encryption is REQUIRED in commercial mode",
				"solution", "Set 'auth-encryption-key' in config or UNIFYAI_AUTH_ENCRYPTION_KEY environment variable")
		} else {
			slog.Error("Failed to initialize token store", "error", err)
		}
		os.Exit(1)
	}
	if cfg.AuthEncryptionKey != "" {
		slog.Info("Token encryption enabled (AES-256-GCM)")
	}

	// Handle login command
	if *loginCmd != "" {
		if err := handleLogin(*loginCmd, tokenStore); err != nil {
			slog.Error("Login failed", "error", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Initialize token manager
	tokenManager := auth.NewTokenManager(tokenStore, auth.DefaultTokenManagerConfig())

	// Load existing accounts
	if err := tokenManager.LoadAllAccounts([]string{"claude", "gemini"}); err != nil {
		slog.Warn("Failed to load some accounts", "error", err)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create HTTP client with proxy if configured
	httpClient := createHTTPClient(cfg)

	// Start auto-refresh service for OAuth tokens
	refreshService := auth.NewAutoRefreshService(tokenManager, httpClient)
	refreshService.Start(ctx)
	slog.Info("Token auto-refresh service started")

	// Create account selector for request routing
	maxRetryInterval := time.Duration(cfg.MaxRetryInterval) * time.Second
	accountSelector := account.NewDefaultAccountSelector(maxRetryInterval)
	slog.Info("Account selector initialized", "max_retry_interval", maxRetryInterval)

	// Populate account selector with accounts from token manager
	// This bridges the TokenManager (auth) and AccountSelector (routing/quota)
	populateAccountSelector(cfg, tokenManager, accountSelector)

	// Initialize metrics collection
	metricsCollector := metrics.New()
	slog.Info("Metrics collector initialized")

	// Initialize providers and transformers with config
	handlers := server.NewHandlersWithConfig(server.HandlersConfig{
		MaxRequestSize:  cfg.MaxRequestSize,
		MaxEventSize:    cfg.MaxEventSize,
		AccountSelector: accountSelector,
		Metrics:         metricsCollector,
	})
	if err := setupProviders(ctx, cfg, tokenStore, handlers); err != nil {
		slog.Error("Failed to setup providers", "error", err)
		os.Exit(1)
	}

	// Set up account reloader for runtime account synchronization
	// This allows reloading accounts via POST /admin/reload-accounts
	handlers.SetAccountReloader(func() {
		// Reload accounts from token store
		if err := tokenManager.LoadAllAccounts([]string{"claude", "gemini"}); err != nil {
			slog.Warn("Failed to reload some accounts", "error", err)
		}
		// Clear existing accounts and repopulate
		accountSelector.Clear()
		populateAccountSelector(configWatcher.GetConfig(), tokenManager, accountSelector)
	})

	// Collect API keys from auth config
	var apiKeys []string
	for _, provider := range cfg.Auth.Providers {
		apiKeys = append(apiKeys, provider.APIKeys...)
	}

	// Build middleware chain
	var middlewares []server.Middleware
	middlewares = append(middlewares, server.RecoveryMiddleware(slog.Default()))
	middlewares = append(middlewares, server.LoggingMiddleware(slog.Default()))

	// Apply rate limiting middleware if enabled
	var rateLimiter *server.RateLimiter
	if cfg.RateLimit.Enabled {
		rateLimiter = server.NewRateLimiter(
			cfg.RateLimit.RequestsPerSecond,
			cfg.RateLimit.BurstSize,
		)
		middlewares = append(middlewares, rateLimiter.Middleware())
		slog.Info("Rate limiting enabled",
			"requests_per_second", cfg.RateLimit.RequestsPerSecond,
			"burst_size", cfg.RateLimit.BurstSize)
	}

	// Apply auth middleware if API keys are configured
	if len(apiKeys) > 0 {
		middlewares = append(middlewares, server.AuthMiddleware(apiKeys))
	}

	// Create and start server with middleware
	srv := server.New(cfg, server.WithMiddleware(middlewares...))
	handlers.RegisterRoutes(srv.Router())

	// Start server in background
	go func() {
		slog.Info("Starting HTTP server",
			"host", cfg.Server.Host,
			"port", cfg.Server.Port)
		if err := srv.Start(); err != nil {
			slog.Error("Server error", "error", err)
			cancel()
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		slog.Info("Received shutdown signal", "signal", sig)
	case <-ctx.Done():
	}

	// Graceful shutdown with timeout
	slog.Info("Shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Stop(shutdownCtx); err != nil {
		slog.Error("Server shutdown error", "error", err)
	}

	// Stop rate limiter cleanup goroutine if enabled
	if rateLimiter != nil {
		rateLimiter.Stop()
	}

	refreshService.Stop()
	tokenManager.Stop()
	configWatcher.Stop()
	slog.Info("Shutdown complete")
}

// handleLogin handles the login command for a provider
func handleLogin(providerName string, store auth.Store) error {
	var flow *auth.OAuthFlow

	switch providerName {
	case "claude":
		flow = auth.NewOAuthFlow(
			provider.ClaudeClientID,
			provider.ClaudeOAuthBaseURL+"/oauth/authorize",
			provider.ClaudeOAuthBaseURL+"/oauth/token",
			[]string{"user:inference", "user:profile"},
			provider.ClaudeCallbackPort,
		)
	case "gemini":
		flow = auth.NewOAuthFlow(
			provider.GeminiClientID,
			provider.GeminiOAuthBaseURL+"/o/oauth2/v2/auth",
			"https://oauth2.googleapis.com/token",
			[]string{
				"https://www.googleapis.com/auth/cloud-platform",
				"https://www.googleapis.com/auth/generative-language",
			},
			provider.GeminiCallbackPort,
		)
	default:
		return fmt.Errorf("unknown provider: %s (supported: claude, gemini)", providerName)
	}

	fmt.Printf("Starting OAuth login for %s...\n", providerName)
	fmt.Println("A browser window will open for authorization.")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	token, err := flow.Login(ctx)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	// Save token with default ID
	if err := store.Save(providerName, "default", token); err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}

	fmt.Printf("Successfully logged in to %s!\n", providerName)
	fmt.Printf("Token saved. Expires at: %s\n", token.ExpiresAt.Format("2006-01-02 15:04:05"))
	return nil
}

// setupProviders initializes and registers providers and transformers
func setupProviders(_ context.Context, cfg *config.Config, tokenStore auth.Store, handlers *server.Handlers) error {
	// Default provider configuration
	defaultConfig := provider.ProviderConfig{
		Timeout:    30,
		MaxRetries: cfg.RequestRetry,
		ProxyURL:   cfg.ProxyURL,
	}

	// Setup Claude provider and transformer
	claudeConfig := defaultConfig
	claudeConfig.BaseURL = provider.ClaudeAPIBaseURL
	claudeProvider := provider.NewClaudeProvider(claudeConfig, tokenStore, "default")
	claudeTransformer := transformer.NewClaudeTransformer()
	handlers.RegisterProvider("claude", claudeProvider)
	handlers.RegisterTransformer("claude", claudeTransformer)

	// Setup Gemini provider and transformer
	geminiConfig := defaultConfig
	geminiConfig.BaseURL = provider.GeminiAPIBaseURL
	geminiProvider := provider.NewGeminiProvider(geminiConfig, tokenStore, "default")
	geminiTransformer := transformer.NewGeminiTransformer()
	handlers.RegisterProvider("gemini", geminiProvider)
	handlers.RegisterTransformer("gemini", geminiTransformer)

	return nil
}


// createHTTPClient creates an HTTP client with optional proxy support
func createHTTPClient(cfg *config.Config) *http.Client {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	// Configure proxy if specified
	if cfg.ProxyURL != "" {
		proxyURL, err := url.Parse(cfg.ProxyURL)
		if err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
			slog.Debug("HTTP client proxy configured", "proxy", cfg.ProxyURL)
		} else {
			slog.Warn("Invalid proxy URL, ignoring", "url", cfg.ProxyURL, "error", err)
		}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
}

// populateAccountSelector creates Account instances from TokenManager and adds them to AccountSelector
// This bridges token management (auth) with request routing and quota management (account)
func populateAccountSelector(cfg *config.Config, tokenManager *auth.TokenManager, selector *account.DefaultAccountSelector) {
	providers := []string{"claude", "gemini"}

	for _, providerName := range providers {
		// Get all account IDs for this provider from token manager
		accountIDs := tokenManager.ListAccounts(providerName)

		for _, accountID := range accountIDs {
			// Create an Account for the selector
			acc := account.NewAccount(accountID, providerName, accountID)

			// Get quota limits from config
			limits := cfg.GetAccountLimits(providerName, accountID)
			acc.SetLimits(limits.Daily.Requests, limits.Daily.Tokens, limits.Rate.RPM)

			// Add to selector
			selector.AddAccount(acc)

			slog.Info("Account registered for routing",
				"provider", providerName,
				"id", accountID,
				"daily_requests", limits.Daily.Requests,
				"daily_tokens", limits.Daily.Tokens,
				"rpm", limits.Rate.RPM)
		}
	}
}