package provider

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Context keys for passing request-specific information
type contextKey string

const (
	// ContextKeyTokenID is the context key for specifying which token/account to use
	// When set, providers will use this token ID instead of their default
	ContextKeyTokenID contextKey = "provider_token_id"
)

// WithTokenID returns a new context with the specified token ID
// This allows handlers to specify which account's token should be used for a request
func WithTokenID(ctx context.Context, tokenID string) context.Context {
	return context.WithValue(ctx, ContextKeyTokenID, tokenID)
}

// GetTokenID extracts the token ID from context, returns empty string if not set
func GetTokenID(ctx context.Context) string {
	if tokenID, ok := ctx.Value(ContextKeyTokenID).(string); ok {
		return tokenID
	}
	return ""
}

// Capability represents provider capabilities
// Reference: docs/unifyai_proxy/15-任务拆解-基础设计.md
type Capability int

const (
	// CapabilityText indicates basic text completion support
	CapabilityText Capability = 1 << iota
	// CapabilityMultimodal indicates multimodal (text + image) support
	CapabilityMultimodal
	// CapabilityToolCalling indicates function/tool calling support
	CapabilityToolCalling
	// CapabilityVision indicates vision/image understanding support
	CapabilityVision
	// CapabilityStreaming indicates streaming output support
	CapabilityStreaming
	// CapabilityThinking indicates thinking/reasoning chain support (e.g., Gemini thinking)
	CapabilityThinking
	// CapabilityCodeExecution indicates code execution support
	CapabilityCodeExecution
)

// String returns the string representation of Capability
func (c Capability) String() string {
	switch c {
	case CapabilityText:
		return "text"
	case CapabilityMultimodal:
		return "multimodal"
	case CapabilityToolCalling:
		return "tool_calling"
	case CapabilityVision:
		return "vision"
	case CapabilityStreaming:
		return "streaming"
	case CapabilityThinking:
		return "thinking"
	case CapabilityCodeExecution:
		return "code_execution"
	default:
		return "unknown"
	}
}

// HasCapability checks if capabilities include a specific capability
func HasCapability(caps []Capability, cap Capability) bool {
	for _, c := range caps {
		if c == cap {
			return true
		}
	}
	return false
}

// HasAllCapabilities checks if capabilities include all the required capabilities
func HasAllCapabilities(caps []Capability, required []Capability) bool {
	for _, req := range required {
		if !HasCapability(caps, req) {
			return false
		}
	}
	return true
}

// CapabilitiesFromBitmask creates a capability slice from a bitmask
func CapabilitiesFromBitmask(mask int) []Capability {
	var caps []Capability
	allCaps := []Capability{
		CapabilityText,
		CapabilityMultimodal,
		CapabilityToolCalling,
		CapabilityVision,
		CapabilityStreaming,
		CapabilityThinking,
		CapabilityCodeExecution,
	}
	for _, cap := range allCaps {
		if mask&int(cap) != 0 {
			caps = append(caps, cap)
		}
	}
	return caps
}

// CapabilitiesToBitmask converts a capability slice to a bitmask
func CapabilitiesToBitmask(caps []Capability) int {
	var mask int
	for _, cap := range caps {
		mask |= int(cap)
	}
	return mask
}

// Provider defines the interface for AI service providers
// Reference: docs/unifyai_proxy/15-任务拆解-基础设计.md
type Provider interface {
	// Name returns the provider name (claude/gemini/codex/qwen)
	Name() string
	// SendRequest sends a synchronous request
	SendRequest(ctx context.Context, req interface{}) (interface{}, error)
	// StreamRequest sends a streaming request, returns a channel of events
	StreamRequest(ctx context.Context, req interface{}) (<-chan StreamEvent, error)
	// Capabilities returns the list of supported capabilities
	Capabilities() []Capability
	// SupportedModels returns the list of supported model names
	SupportedModels() []string
	// SupportsModel checks if a specific model is supported
	SupportsModel(model string) bool
}

// StreamEvent represents a streaming event from a provider
type StreamEvent struct {
	Type    StreamEventType
	Data    interface{}
	Error   error
	RawData []byte
}

// StreamEventType represents types of streaming events
type StreamEventType int

const (
	// StreamEventTypeData indicates a data chunk
	StreamEventTypeData StreamEventType = iota
	// StreamEventTypeError indicates an error occurred
	StreamEventTypeError
	// StreamEventTypeDone indicates stream completion
	StreamEventTypeDone
	// StreamEventTypeToolCall indicates a tool call event
	StreamEventTypeToolCall
	// StreamEventTypeThinking indicates a thinking/reasoning event
	StreamEventTypeThinking
)

// OAuthProvider extends Provider with OAuth-specific configuration
type OAuthProvider interface {
	Provider
	// OAuthConfig returns the OAuth configuration
	OAuthConfig() OAuthConfig
	// RefreshToken refreshes the OAuth token
	RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error)
	// GetValidToken returns a valid access token, refreshing if necessary
	GetValidToken(ctx context.Context) (string, error)
}

// OAuthConfig defines OAuth provider configuration
// Reference: docs/unifyai_proxy/15-任务拆解-基础设计.md
type OAuthConfig struct {
	// CallbackPort is the local port for OAuth callback
	CallbackPort int
	// CallbackPortType indicates if port is "fixed" or "dynamic"
	CallbackPortType string
	// RequiresPKCE indicates if PKCE is required
	RequiresPKCE bool
	// ClientID is the OAuth client ID
	ClientID string
	// ClientSecret is the OAuth client secret (if applicable)
	ClientSecret string
	// Scopes are the OAuth scopes to request
	Scopes []string
	// AuthorizeURL is the authorization endpoint
	AuthorizeURL string
	// TokenURL is the token endpoint
	TokenURL string
	// RedirectURI is the redirect URI template
	RedirectURI string
}

// TokenResponse represents an OAuth token response
type TokenResponse struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64  // seconds until expiration
	TokenType    string
	Scope        string
}

// APIKeyProvider extends Provider for API key-based authentication
type APIKeyProvider interface {
	Provider
	// SetAPIKey sets the API key for requests
	SetAPIKey(apiKey string)
	// GetAPIKey returns the current API key
	GetAPIKey() string
}

// ProviderFactory creates Provider instances
type ProviderFactory interface {
	// Create creates a new provider instance
	Create(providerType string, config interface{}) (Provider, error)
	// SupportedProviders returns the list of supported provider types
	SupportedProviders() []string
}

// Common errors
var (
	ErrProviderNotFound     = errors.New("provider not found")
	ErrModelNotSupported    = errors.New("model not supported by this provider")
	ErrCapabilityNotSupported = errors.New("capability not supported by this provider")
	ErrAuthenticationFailed = errors.New("authentication failed")
	ErrTokenExpired         = errors.New("token expired")
	ErrRateLimited          = errors.New("rate limited")
	ErrQuotaExceeded        = errors.New("quota exceeded")
	ErrServerError          = errors.New("server error")
	ErrNetworkError         = errors.New("network error")
	ErrStreamClosed         = errors.New("stream closed")
)


// ProviderType represents the type of provider
type ProviderType string

const (
	ProviderTypeClaude  ProviderType = "claude"
	ProviderTypeGemini  ProviderType = "gemini"
	ProviderTypeCodex   ProviderType = "codex"
	ProviderTypeQwen    ProviderType = "qwen"
	ProviderTypeOpenAI  ProviderType = "openai"
	ProviderTypeVertex  ProviderType = "vertex"
	ProviderTypeAmpcode ProviderType = "ampcode"
)

// String returns the string representation of ProviderType
func (p ProviderType) String() string {
	return string(p)
}

// ProviderConfig contains common provider configuration
type ProviderConfig struct {
	// BaseURL is the base URL for API requests
	BaseURL string
	// ProxyURL is the proxy URL for requests
	ProxyURL string
	// Headers are additional headers to include in requests
	Headers map[string]string
	// Timeout is the request timeout in seconds
	Timeout int
	// MaxRetries is the maximum number of retries
	MaxRetries int
	// Models is the list of supported models (empty means all)
	Models []string
	// ExcludedModels is the list of excluded models
	ExcludedModels []string
}

// DefaultProviderFactory is a basic implementation of ProviderFactory
type DefaultProviderFactory struct {
	creators map[string]ProviderCreator
	mu       sync.RWMutex
}

// ProviderCreator is a function that creates a provider instance
type ProviderCreator func(config interface{}) (Provider, error)

// NewDefaultProviderFactory creates a new default provider factory
func NewDefaultProviderFactory() *DefaultProviderFactory {
	return &DefaultProviderFactory{
		creators: make(map[string]ProviderCreator),
	}
}

// Register registers a provider creator
func (f *DefaultProviderFactory) Register(providerType string, creator ProviderCreator) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creators[providerType] = creator
}

// Unregister removes a provider creator
func (f *DefaultProviderFactory) Unregister(providerType string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.creators, providerType)
}

// Create creates a new provider instance
func (f *DefaultProviderFactory) Create(providerType string, config interface{}) (Provider, error) {
	f.mu.RLock()
	creator, exists := f.creators[providerType]
	f.mu.RUnlock()

	if !exists {
		return nil, ErrProviderNotFound
	}
	return creator(config)
}

// SupportedProviders returns the list of supported provider types
func (f *DefaultProviderFactory) SupportedProviders() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	providers := make([]string, 0, len(f.creators))
	for name := range f.creators {
		providers = append(providers, name)
	}
	return providers
}

// HasProvider checks if a provider type is registered
func (f *DefaultProviderFactory) HasProvider(providerType string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, exists := f.creators[providerType]
	return exists
}

// ModelInfo contains information about a model
type ModelInfo struct {
	Name           string
	Provider       ProviderType
	Capabilities   []Capability
	MaxContextSize int
	MaxOutputSize  int
	IsPreview      bool
	Aliases        []string
}

// ProviderStatus represents the status of a provider
type ProviderStatus struct {
	Name            string
	Available       bool
	AccountCount    int
	AvailableCount  int
	LastError       error
	LastRequestTime int64
}

// Request/Response wrapper types for provider-agnostic handling

// ProviderRequest wraps a provider-specific request
type ProviderRequest struct {
	Model       string
	Messages    interface{}
	Stream      bool
	MaxTokens   int
	Temperature float64
	TopP        float64
	Tools       interface{}
	Metadata    map[string]interface{}
}

// ProviderResponse wraps a provider-specific response
type ProviderResponse struct {
	ID           string
	Model        string
	Content      interface{}
	Usage        Usage
	FinishReason string
	Error        *ProviderError
}

// Usage contains token usage information
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// ProviderError represents a provider-specific error
type ProviderError struct {
	Type       string
	Message    string
	Code       string
	StatusCode int
	Retryable  bool
}

// Error implements the error interface
func (e *ProviderError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s: %s (code: %s)", e.Type, e.Message, e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}
