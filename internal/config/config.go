package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration structure.
// Reference: docs/unifyai_proxy/15-任务拆解-基础设计.md and docs/unifyai_proxy/04-配置参考.md
type Config struct {
	Server           ServerConfig           `yaml:"server"`
	TLS              TLSConfig              `yaml:"tls"`
	RemoteManagement RemoteManagementConfig `yaml:"remote-management"`
	Auth             AuthConfig             `yaml:"auth"`
	AuthDir          string                 `yaml:"auth-dir"`
	AuthEncryptionKey string                `yaml:"auth-encryption-key"` // Encryption key for token storage
	Logging          LoggingConfig          `yaml:"logging"`
	LoggingToFile    bool                   `yaml:"logging-to-file"`
	LogsMaxTotalSize int                    `yaml:"logs-max-total-size-mb"`
	CommercialMode   bool                   `yaml:"commercial-mode"`
	ProxyURL         string                 `yaml:"proxy-url"`
	RequestRetry     int                    `yaml:"request-retry"`
	MaxRetryInterval int                    `yaml:"max-retry-interval"`
	MaxRequestSize   int64                  `yaml:"max-request-size"`  // Max request body size in bytes (default: 10MB)
	MaxEventSize     int                    `yaml:"max-event-size"`    // Max SSE event size in bytes (default: 1MB)
	RateLimit        GlobalRateLimitConfig  `yaml:"rate-limit"`        // Global rate limit configuration
	QuotaExceeded    QuotaExceededConfig    `yaml:"quota-exceeded"`
	Routing          RoutingConfig          `yaml:"routing"`
	Streaming        StreamingConfig        `yaml:"streaming"`
	AccountLimits    AccountLimitsConfig    `yaml:"account-limits"`
	ModelMapping     map[string]string      `yaml:"model-mapping"`
	ModelBlacklist   []string               `yaml:"model-blacklist"`
	ForceModelPrefix bool                   `yaml:"force-model-prefix"`
	WSAuth           bool                   `yaml:"ws-auth"`
	UsageStatistics  bool                   `yaml:"usage-statistics-enabled"`

	// OAuth model configurations
	OAuthModelMappings  map[string][]ModelAliasConfig `yaml:"oauth-model-mappings"`
	OAuthExcludedModels map[string][]string           `yaml:"oauth-excluded-models"`

	// Payload rules
	Payload PayloadConfig `yaml:"payload"`

	// API Key configurations for direct access (bypass OAuth)
	GeminiAPIKey []APIKeyEntryConfig `yaml:"gemini-api-key"`
	ClaudeAPIKey []APIKeyEntryConfig `yaml:"claude-api-key"`
	CodexAPIKey  []APIKeyEntryConfig `yaml:"codex-api-key"`
	VertexAPIKey []APIKeyEntryConfig `yaml:"vertex-api-key"`

	// OpenAI compatible upstream
	OpenAICompatibility []OpenAICompatConfig `yaml:"openai-compatibility"`

	// Ampcode configuration
	Ampcode AmpcodeConfig `yaml:"ampcode"`
}

// ServerConfig defines server binding settings
type ServerConfig struct {
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	AllowRemote  bool   `yaml:"allow-remote"`
	SecretKey    string `yaml:"secret-key"`
	ReadTimeout  int    `yaml:"read-timeout"`  // seconds
	WriteTimeout int    `yaml:"write-timeout"` // seconds
	IdleTimeout  int    `yaml:"idle-timeout"`  // seconds
}

// TLSConfig defines TLS settings
type TLSConfig struct {
	Enable   bool   `yaml:"enable"`
	CertFile string `yaml:"cert"`
	KeyFile  string `yaml:"key"`
}

// RemoteManagementConfig defines remote management settings
type RemoteManagementConfig struct {
	AllowRemote         bool   `yaml:"allow-remote"`
	SecretKey           string `yaml:"secret-key"`
	DisableControlPanel bool   `yaml:"disable-control-panel"`
	PanelGitHubRepo     string `yaml:"panel-github-repository"`
}

// AuthConfig defines authentication settings
type AuthConfig struct {
	Providers []AuthProviderConfig `yaml:"providers"`
}

// AuthProviderConfig defines an authentication provider
type AuthProviderConfig struct {
	Type    string   `yaml:"type"`
	APIKeys []string `yaml:"api-keys"`
}

// LoggingConfig defines logging settings
type LoggingConfig struct {
	Level     string `yaml:"level"`
	Directory string `yaml:"directory"`
}

// QuotaExceededConfig defines behavior when quota is exceeded
type QuotaExceededConfig struct {
	SwitchProject      bool `yaml:"switch-project"`
	SwitchPreviewModel bool `yaml:"switch-preview-model"`
}

// RoutingConfig defines routing strategy
type RoutingConfig struct {
	Strategy string `yaml:"strategy"` // "round-robin" or "fill-first"
}

// StreamingConfig defines SSE streaming settings
type StreamingConfig struct {
	KeepaliveSeconds  int `yaml:"keepalive-seconds"`
	BootstrapRetries  int `yaml:"bootstrap-retries"`
}

// AccountLimitsConfig defines account quota limits
type AccountLimitsConfig struct {
	ResetTimezone string                       `yaml:"reset-timezone"`
	Default       AccountLimitEntry            `yaml:"default"`
	Providers     map[string]AccountLimitEntry `yaml:"providers"`
	Overrides     []AccountLimitOverride       `yaml:"overrides"`
}

// AccountLimitEntry defines daily and rate limits
type AccountLimitEntry struct {
	Daily DailyLimits `yaml:"daily"`
	Rate  RateLimits  `yaml:"rate"`
}

// DailyLimits defines daily quota limits
type DailyLimits struct {
	Requests int64 `yaml:"requests"`
	Tokens   int64 `yaml:"tokens"`
}

// RateLimits defines rate limits
type RateLimits struct {
	RPM int `yaml:"rpm"` // Requests per minute
}

// GlobalRateLimitConfig defines global rate limiting configuration
type GlobalRateLimitConfig struct {
	Enabled           bool    `yaml:"enabled"`             // Enable rate limiting
	RequestsPerSecond float64 `yaml:"requests-per-second"` // Requests per second per client
	BurstSize         int     `yaml:"burst-size"`          // Maximum burst size
}

// AccountLimitOverride allows per-account limit overrides
type AccountLimitOverride struct {
	ID    string      `yaml:"id"`
	Daily DailyLimits `yaml:"daily"`
	Rate  RateLimits  `yaml:"rate"`
}

// ModelAliasConfig defines model alias mapping
type ModelAliasConfig struct {
	Name  string `yaml:"name"`
	Alias string `yaml:"alias"`
}

// PayloadConfig defines request payload rules
type PayloadConfig struct {
	Default  []PayloadRule `yaml:"default"`
	Override []PayloadRule `yaml:"override"`
}

// PayloadRule defines a payload modification rule
type PayloadRule struct {
	Models []PayloadModelMatch    `yaml:"models"`
	Params map[string]interface{} `yaml:"params"`
}

// PayloadModelMatch defines model matching criteria
type PayloadModelMatch struct {
	Name     string `yaml:"name"`
	Protocol string `yaml:"protocol"`
}


// APIKeyEntryConfig defines an API key entry for direct provider access
type APIKeyEntryConfig struct {
	APIKey         string            `yaml:"api-key"`
	Prefix         string            `yaml:"prefix"`
	BaseURL        string            `yaml:"base-url"`
	Headers        map[string]string `yaml:"headers"`
	ProxyURL       string            `yaml:"proxy-url"`
	Models         []string          `yaml:"models"`
	ExcludedModels []string          `yaml:"excluded-models"`
}

// OpenAICompatConfig defines an OpenAI-compatible upstream
type OpenAICompatConfig struct {
	Name          string            `yaml:"name"`
	Prefix        string            `yaml:"prefix"`
	BaseURL       string            `yaml:"base-url"`
	Headers       map[string]string `yaml:"headers"`
	APIKeyEntries []string          `yaml:"api-key-entries"`
	Models        []string          `yaml:"models"`
}

// AmpcodeConfig defines Ampcode integration settings
type AmpcodeConfig struct {
	Enabled bool              `yaml:"enabled"`
	BaseURL string            `yaml:"base-url"`
	Headers map[string]string `yaml:"headers"`
}

// DefaultConfig returns a Config with default values
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:         "127.0.0.1",
			Port:         8317,
			AllowRemote:  false,
			ReadTimeout:  30,  // 30 seconds
			WriteTimeout: 30,  // 30 seconds
			IdleTimeout:  120, // 2 minutes
		},
		TLS: TLSConfig{
			Enable: false,
		},
		RemoteManagement: RemoteManagementConfig{
			AllowRemote:         false,
			DisableControlPanel: false,
			PanelGitHubRepo:     "https://github.com/router-for-me/Cli-Proxy-API-Management-Center",
		},
		AuthDir:          "~/.unifyai-proxy/auths",
		MaxRequestSize:   10 * 1024 * 1024, // 10MB default
		MaxEventSize:     1024 * 1024,      // 1MB default for SSE events
		RateLimit: GlobalRateLimitConfig{
			Enabled:           false,            // Disabled by default
			RequestsPerSecond: 10,               // 10 requests per second
			BurstSize:         20,               // Burst of 20
		},
		Logging: LoggingConfig{
			Level:     "info",
			Directory: "./logs",
		},
		LoggingToFile:    false,
		LogsMaxTotalSize: 0,
		CommercialMode:   false,
		RequestRetry:     3,
		MaxRetryInterval: 30,
		QuotaExceeded: QuotaExceededConfig{
			SwitchProject:      true,
			SwitchPreviewModel: true,
		},
		Routing: RoutingConfig{
			Strategy: "round-robin",
		},
		Streaming: StreamingConfig{
			KeepaliveSeconds: 0,
			BootstrapRetries: 0,
		},
		AccountLimits: AccountLimitsConfig{
			ResetTimezone: "UTC",
		},
		ModelMapping:   make(map[string]string),
		ModelBlacklist: []string{},
		WSAuth:         false,
		UsageStatistics: false,
	}
}

// Load parses configuration from a file path and applies env overrides.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return default config if file doesn't exist
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Apply environment variable overrides
	applyEnvOverrides(cfg)

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// Validate checks configuration for errors
func (c *Config) Validate() error {
	// Validate server configuration
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}

	// Validate TLS configuration
	if c.TLS.Enable {
		if c.TLS.CertFile == "" {
			return fmt.Errorf("TLS enabled but cert file not specified")
		}
		if c.TLS.KeyFile == "" {
			return fmt.Errorf("TLS enabled but key file not specified")
		}
		if _, err := os.Stat(c.TLS.CertFile); os.IsNotExist(err) {
			return fmt.Errorf("TLS cert file not found: %s", c.TLS.CertFile)
		}
		if _, err := os.Stat(c.TLS.KeyFile); os.IsNotExist(err) {
			return fmt.Errorf("TLS key file not found: %s", c.TLS.KeyFile)
		}
	}

	// Validate logging level
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[c.Logging.Level] {
		return fmt.Errorf("invalid logging level: %s", c.Logging.Level)
	}

	// Validate routing strategy
	validStrategies := map[string]bool{"round-robin": true, "fill-first": true}
	if c.Routing.Strategy != "" && !validStrategies[c.Routing.Strategy] {
		return fmt.Errorf("invalid routing strategy: %s", c.Routing.Strategy)
	}

	// Validate model alias chain (detect cycles and max depth)
	if err := c.validateModelAliases(); err != nil {
		return err
	}

	return nil
}

// validateModelAliases checks for circular references and excessive depth in model aliases
func (c *Config) validateModelAliases() error {
	const maxDepth = 5

	for alias := range c.ModelMapping {
		visited := make(map[string]bool)
		current := alias
		depth := 0

		for {
			if visited[current] {
				return fmt.Errorf("circular model alias detected: %s", alias)
			}
			visited[current] = true
			depth++

			if depth > maxDepth {
				return fmt.Errorf("model alias chain too deep (>%d): %s", maxDepth, alias)
			}

			next, exists := c.ModelMapping[current]
			if !exists {
				break
			}
			current = next
		}
	}

	return nil
}

// applyEnvOverrides applies environment variable overrides to config
func applyEnvOverrides(cfg *Config) {
	if host := os.Getenv("UNIFYAI_HOST"); host != "" {
		cfg.Server.Host = host
	}
	if port := os.Getenv("UNIFYAI_PORT"); port != "" {
		var p int
		if _, err := fmt.Sscanf(port, "%d", &p); err == nil {
			cfg.Server.Port = p
		}
	}
	if secret := os.Getenv("UNIFYAI_SECRET_KEY"); secret != "" {
		cfg.Server.SecretKey = secret
	}
	if proxy := os.Getenv("UNIFYAI_PROXY_URL"); proxy != "" {
		cfg.ProxyURL = proxy
	}
	if logLevel := os.Getenv("UNIFYAI_LOG_LEVEL"); logLevel != "" {
		cfg.Logging.Level = logLevel
	}
	// Security: Token encryption key (highly recommended for production)
	if encKey := os.Getenv("UNIFYAI_AUTH_ENCRYPTION_KEY"); encKey != "" {
		cfg.AuthEncryptionKey = encKey
	}
}
