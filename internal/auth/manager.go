package auth

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// OAuth client IDs for token refresh
// These MUST match the provider configurations to ensure refresh works correctly
const (
	// ClaudeOAuthClientID is the OAuth client ID for Claude (claude-code-cli)
	ClaudeOAuthClientID = "claude-code-cli"
	// ClaudeOAuthTokenURL is the token endpoint for Claude
	ClaudeOAuthTokenURL = "https://claude.ai/oauth/token"

	// GeminiOAuthClientID is the OAuth client ID for Gemini
	GeminiOAuthClientID = "667607542161-gua21ma1uangl0mphq8rm62rpi1klh6h.apps.googleusercontent.com"
	// GeminiOAuthTokenURL is the token endpoint for Gemini
	GeminiOAuthTokenURL = "https://oauth2.googleapis.com/token"
)

// TokenManager manages tokens for multiple accounts and providers
type TokenManager struct {
	store           Store
	refreshInterval time.Duration
	refreshBefore   time.Duration // refresh this much before expiration
	accounts        map[string]*ManagedAccount // provider:id -> account
	mu              sync.RWMutex
	stopCh          chan struct{}
	wg              sync.WaitGroup
	stopOnce        sync.Once // Prevents double-close panic
}

// ManagedAccount represents an account managed by TokenManager
type ManagedAccount struct {
	Provider     string
	ID           string
	Token        *Token
	LastRefresh  time.Time
	RefreshError error
	IsRefreshing bool
	mu           sync.RWMutex
}

// TokenManagerConfig contains configuration for TokenManager
type TokenManagerConfig struct {
	RefreshInterval time.Duration // How often to check for expiring tokens
	RefreshBefore   time.Duration // Refresh tokens this much before expiration
}

// DefaultTokenManagerConfig returns default configuration
func DefaultTokenManagerConfig() TokenManagerConfig {
	return TokenManagerConfig{
		RefreshInterval: 5 * time.Minute,
		RefreshBefore:   10 * time.Minute,
	}
}

// NewTokenManager creates a new TokenManager
func NewTokenManager(store Store, config TokenManagerConfig) *TokenManager {
	return &TokenManager{
		store:           store,
		refreshInterval: config.RefreshInterval,
		refreshBefore:   config.RefreshBefore,
		accounts:        make(map[string]*ManagedAccount),
		stopCh:          make(chan struct{}),
	}
}

// accountKey generates a unique key for provider:id
func accountKey(provider, id string) string {
	return provider + ":" + id
}

// AddAccount adds an account to be managed
func (tm *TokenManager) AddAccount(provider, id string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	key := accountKey(provider, id)
	if _, exists := tm.accounts[key]; exists {
		return nil // Already exists
	}

	// Try to load existing token
	token, err := tm.store.Load(provider, id)
	if err != nil && err != ErrTokenNotFound {
		return fmt.Errorf("failed to load token: %w", err)
	}

	tm.accounts[key] = &ManagedAccount{
		Provider: provider,
		ID:       id,
		Token:    token,
	}

	return nil
}

// RemoveAccount removes an account from management
func (tm *TokenManager) RemoveAccount(provider, id string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	delete(tm.accounts, accountKey(provider, id))
}

// GetToken returns a valid token for the specified account
func (tm *TokenManager) GetToken(provider, id string) (*Token, error) {
	tm.mu.RLock()
	acc, exists := tm.accounts[accountKey(provider, id)]
	tm.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("account not found: %s/%s", provider, id)
	}

	acc.mu.RLock()
	token := acc.Token
	acc.mu.RUnlock()

	if token == nil {
		return nil, ErrTokenNotFound
	}

	if !token.IsValid() {
		return nil, ErrInvalidToken
	}

	return token, nil
}

// SaveToken saves a token for an account
func (tm *TokenManager) SaveToken(provider, id string, token *Token) error {
	if err := tm.store.Save(provider, id, token); err != nil {
		return err
	}

	tm.mu.Lock()
	key := accountKey(provider, id)
	if acc, exists := tm.accounts[key]; exists {
		acc.mu.Lock()
		acc.Token = token
		acc.mu.Unlock()
	} else {
		tm.accounts[key] = &ManagedAccount{
			Provider: provider,
			ID:       id,
			Token:    token,
		}
	}
	tm.mu.Unlock()

	return nil
}

// DeleteToken deletes a token for an account
func (tm *TokenManager) DeleteToken(provider, id string) error {
	if err := tm.store.Delete(provider, id); err != nil {
		return err
	}

	tm.mu.Lock()
	if acc, exists := tm.accounts[accountKey(provider, id)]; exists {
		acc.mu.Lock()
		acc.Token = nil
		acc.mu.Unlock()
	}
	tm.mu.Unlock()

	return nil
}

// ListAccounts returns all account IDs for a provider
func (tm *TokenManager) ListAccounts(provider string) []string {
	return tm.listAccountsFromStore(provider)
}

func (tm *TokenManager) listAccountsFromStore(provider string) []string {
	ids, _ := tm.store.List(provider)
	return ids
}

// RefreshFunc is a function that refreshes a token
type RefreshFunc func(ctx context.Context, provider, id, refreshToken string) (*Token, error)

// StartAutoRefresh starts the automatic token refresh background goroutine
func (tm *TokenManager) StartAutoRefresh(ctx context.Context, refreshFn RefreshFunc) {
	tm.wg.Add(1)
	go tm.autoRefreshLoop(ctx, refreshFn)
}

// Stop stops the token manager and waits for background goroutines
// Safe to call multiple times (uses sync.Once internally)
func (tm *TokenManager) Stop() {
	tm.stopOnce.Do(func() {
		close(tm.stopCh)
	})
	tm.wg.Wait()
}

// autoRefreshLoop periodically checks and refreshes expiring tokens
func (tm *TokenManager) autoRefreshLoop(ctx context.Context, refreshFn RefreshFunc) {
	defer tm.wg.Done()

	ticker := time.NewTicker(tm.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tm.stopCh:
			return
		case <-ticker.C:
			tm.checkAndRefreshTokens(ctx, refreshFn)
		}
	}
}

// checkAndRefreshTokens checks all tokens and refreshes those about to expire
func (tm *TokenManager) checkAndRefreshTokens(ctx context.Context, refreshFn RefreshFunc) {
	tm.mu.RLock()
	accounts := make([]*ManagedAccount, 0, len(tm.accounts))
	for _, acc := range tm.accounts {
		accounts = append(accounts, acc)
	}
	tm.mu.RUnlock()

	for _, acc := range accounts {
		tm.checkAndRefreshAccount(ctx, acc, refreshFn)
	}
}

// checkAndRefreshAccount checks and refreshes a single account's token
func (tm *TokenManager) checkAndRefreshAccount(ctx context.Context, acc *ManagedAccount, refreshFn RefreshFunc) {
	acc.mu.Lock()
	if acc.IsRefreshing {
		acc.mu.Unlock()
		return
	}

	if acc.Token == nil {
		acc.mu.Unlock()
		return
	}

	// Check if token needs refresh
	timeUntilExpiry := time.Until(acc.Token.ExpiresAt)
	if timeUntilExpiry > tm.refreshBefore {
		acc.mu.Unlock()
		return
	}

	if acc.Token.RefreshToken == "" {
		acc.mu.Unlock()
		return
	}

	acc.IsRefreshing = true
	refreshToken := acc.Token.RefreshToken
	acc.mu.Unlock()

	// Perform refresh
	newToken, err := refreshFn(ctx, acc.Provider, acc.ID, refreshToken)

	acc.mu.Lock()
	acc.IsRefreshing = false
	acc.LastRefresh = time.Now()

	if err != nil {
		acc.RefreshError = err
		slog.Error("token refresh failed",
			"provider", acc.Provider,
			"id", acc.ID,
			"error", err)
		acc.mu.Unlock()
		return
	}

	acc.Token = newToken
	acc.RefreshError = nil
	acc.mu.Unlock()

	// Save the new token
	if err := tm.store.Save(acc.Provider, acc.ID, newToken); err != nil {
		slog.Error("failed to save refreshed token",
			"provider", acc.Provider,
			"id", acc.ID,
			"error", err)
	} else {
		slog.Info("token refreshed successfully",
			"provider", acc.Provider,
			"id", acc.ID)
	}
}

// GetAccountStatus returns the status of an account
func (tm *TokenManager) GetAccountStatus(provider, id string) *AccountStatus {
	tm.mu.RLock()
	acc, exists := tm.accounts[accountKey(provider, id)]
	tm.mu.RUnlock()

	if !exists {
		return nil
	}

	acc.mu.RLock()
	defer acc.mu.RUnlock()

	status := &AccountStatus{
		Provider:     acc.Provider,
		ID:           acc.ID,
		HasToken:     acc.Token != nil,
		LastRefresh:  acc.LastRefresh,
		RefreshError: acc.RefreshError,
		IsRefreshing: acc.IsRefreshing,
	}

	if acc.Token != nil {
		status.IsValid = acc.Token.IsValid()
		status.ExpiresAt = acc.Token.ExpiresAt
	}

	return status
}

// AccountStatus represents the current status of a managed account
type AccountStatus struct {
	Provider     string
	ID           string
	HasToken     bool
	IsValid      bool
	ExpiresAt    time.Time
	LastRefresh  time.Time
	RefreshError error
	IsRefreshing bool
}

// LoadAllAccounts loads all accounts from store for the given providers
func (tm *TokenManager) LoadAllAccounts(providers []string) error {
	for _, provider := range providers {
		ids, err := tm.store.List(provider)
		if err != nil {
			continue
		}
		for _, id := range ids {
			if err := tm.AddAccount(provider, id); err != nil {
				slog.Warn("failed to load account",
					"provider", provider,
					"id", id,
					"error", err)
			}
		}
	}
	return nil
}

// GetAvailableAccount returns the first available account for a provider
func (tm *TokenManager) GetAvailableAccount(provider string) (string, *Token, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for key, acc := range tm.accounts {
		if acc.Provider != provider {
			continue
		}

		acc.mu.RLock()
		token := acc.Token
		id := acc.ID
		acc.mu.RUnlock()

		if token != nil && token.IsValid() {
			return id, token, nil
		}

		// Skip this account
		_ = key
	}

	return "", nil, fmt.Errorf("no available account for provider: %s", provider)
}

// Account represents an account for external use
type Account struct {
	Provider string
	ID       string
	Token    *Token
}

// GetAllAccounts returns all managed accounts
func (tm *TokenManager) GetAllAccounts() []*Account {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	accounts := make([]*Account, 0, len(tm.accounts))
	for _, acc := range tm.accounts {
		acc.mu.RLock()
		accounts = append(accounts, &Account{
			Provider: acc.Provider,
			ID:       acc.ID,
			Token:    acc.Token,
		})
		acc.mu.RUnlock()
	}
	return accounts
}

// RefreshToken refreshes the token for a specific account using HTTP client
func (tm *TokenManager) RefreshToken(ctx context.Context, provider, id string, httpClient interface{}) error {
	tm.mu.RLock()
	acc, exists := tm.accounts[accountKey(provider, id)]
	tm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("account not found: %s/%s", provider, id)
	}

	acc.mu.Lock()
	if acc.Token == nil || acc.Token.RefreshToken == "" {
		acc.mu.Unlock()
		return fmt.Errorf("no refresh token available for %s/%s", provider, id)
	}
	refreshToken := acc.Token.RefreshToken
	acc.mu.Unlock()

	// Create refresh function based on provider
	var tokenURL string
	var clientID string

	switch provider {
	case "claude":
		tokenURL = ClaudeOAuthTokenURL
		clientID = ClaudeOAuthClientID
	case "gemini":
		tokenURL = GeminiOAuthTokenURL
		clientID = GeminiOAuthClientID
	default:
		return fmt.Errorf("unsupported provider: %s", provider)
	}

	// Perform the refresh
	newToken, err := refreshOAuthToken(ctx, httpClient, tokenURL, clientID, refreshToken)
	if err != nil {
		return fmt.Errorf("refresh failed: %w", err)
	}

	// Update the token
	acc.mu.Lock()
	acc.Token = newToken
	acc.LastRefresh = time.Now()
	acc.RefreshError = nil
	acc.mu.Unlock()

	// Save to store
	return tm.store.Save(provider, id, newToken)
}

// HTTPDoer is an interface for HTTP client that can perform requests
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}
