package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/unifyai-proxy/unifyai-proxy/internal/auth"
	"github.com/unifyai-proxy/unifyai-proxy/internal/util"
)

// Claude API constants
const (
	ClaudeAPIBaseURL   = "https://api.anthropic.com"
	ClaudeOAuthBaseURL = "https://claude.ai"
	ClaudeAPIVersion   = "2023-06-01"
	// ClaudeClientID references auth.ClaudeOAuthClientID for consistency
	ClaudeClientID     = auth.ClaudeOAuthClientID
	ClaudeCallbackPort = 54545
	ClaudeUserAgent    = "claude-code/1.0.0"
	DefaultMaxTokens   = 4096

	// MaxErrorResponseSize is the maximum size for error response bodies (64KB)
	// This prevents memory exhaustion from malicious or buggy upstream servers
	MaxErrorResponseSize = 64 * 1024
)

// ClaudeProvider implements the Provider interface for Claude/Anthropic
type ClaudeProvider struct {
	config     ProviderConfig
	oauthCfg   OAuthConfig
	httpClient *http.Client
	tokenStore auth.Store
	tokenID    string // identifier for this provider's token
	mu         sync.RWMutex
}

// NewClaudeProvider creates a new Claude provider instance
func NewClaudeProvider(config ProviderConfig, tokenStore auth.Store, tokenID string) *ClaudeProvider {
	client := &http.Client{
		Timeout: time.Duration(config.Timeout) * time.Second,
	}

	// Configure proxy if specified
	if config.ProxyURL != "" {
		proxyURL, err := url.Parse(config.ProxyURL)
		if err != nil {
			// Log proxy URL parse error - this is a configuration issue that should be visible
			slog.Error("failed to parse proxy URL for Claude provider, proxy will not be used",
				"proxy_url", config.ProxyURL,
				"error", err)
		} else {
			client.Transport = &http.Transport{
				Proxy:                 http.ProxyURL(proxyURL),
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   10,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			}
			slog.Debug("proxy configured for Claude provider", "proxy_url", config.ProxyURL)
		}
	}

	return &ClaudeProvider{
		config:     config,
		oauthCfg:   ClaudeOAuthConfigDefault(),
		httpClient: client,
		tokenStore: tokenStore,
		tokenID:    tokenID,
	}
}

// ClaudeOAuthConfigDefault returns the default OAuth config for Claude
func ClaudeOAuthConfigDefault() OAuthConfig {
	return OAuthConfig{
		CallbackPort:     ClaudeCallbackPort,
		CallbackPortType: "fixed",
		RequiresPKCE:     true,
		ClientID:         ClaudeClientID,
		Scopes:           []string{"user:inference", "user:profile"},
		AuthorizeURL:     ClaudeOAuthBaseURL + "/oauth/authorize",
		TokenURL:         ClaudeOAuthBaseURL + "/oauth/token",
		RedirectURI:      fmt.Sprintf("http://localhost:%d/callback", ClaudeCallbackPort),
	}
}

// Name returns the provider name
func (p *ClaudeProvider) Name() string {
	return string(ProviderTypeClaude)
}

// Capabilities returns the supported capabilities
func (p *ClaudeProvider) Capabilities() []Capability {
	return []Capability{
		CapabilityText,
		CapabilityMultimodal,
		CapabilityToolCalling,
		CapabilityVision,
		CapabilityStreaming,
		CapabilityThinking,
	}
}

// SupportedModels returns the list of supported Claude models
func (p *ClaudeProvider) SupportedModels() []string {
	if len(p.config.Models) > 0 {
		return p.config.Models
	}
	return []string{
		"claude-sonnet-4-5-20250929",
		"claude-opus-4-20250514",
		"claude-3-5-sonnet-20241022",
		"claude-3-5-haiku-20241022",
		"claude-3-opus-20240229",
		"claude-3-sonnet-20240229",
		"claude-3-haiku-20240307",
	}
}

// SupportsModel checks if a specific model is supported
func (p *ClaudeProvider) SupportsModel(model string) bool {
	// Check excluded models
	for _, excluded := range p.config.ExcludedModels {
		if excluded == model || util.MatchWildcard(excluded, model) {
			return false
		}
	}

	// If specific models are configured, check against them
	if len(p.config.Models) > 0 {
		for _, m := range p.config.Models {
			if m == model || util.MatchWildcard(m, model) {
				return true
			}
		}
		return false
	}

	// Check if it's a known Claude model
	return strings.HasPrefix(model, "claude-")
}

// OAuthConfig returns the OAuth configuration
func (p *ClaudeProvider) OAuthConfig() OAuthConfig {
	return p.oauthCfg
}

// GetValidToken returns a valid access token, refreshing if necessary
func (p *ClaudeProvider) GetValidToken(ctx context.Context) (string, error) {
	p.mu.RLock()
	token, err := p.tokenStore.Load("claude", p.tokenID)
	p.mu.RUnlock()

	if err != nil {
		return "", fmt.Errorf("failed to load token: %w", err)
	}

	if token.IsValid() {
		return token.AccessToken, nil
	}

	// Token expired, try to refresh
	if token.RefreshToken != "" {
		newToken, err := p.RefreshToken(ctx, token.RefreshToken)
		if err != nil {
			return "", fmt.Errorf("failed to refresh token: %w", err)
		}
		return newToken.AccessToken, nil
	}

	return "", ErrTokenExpired
}

// RefreshToken refreshes the OAuth token
func (p *ClaudeProvider) RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", p.oauthCfg.ClientID)

	req, err := http.NewRequestWithContext(ctx, "POST", p.oauthCfg.TokenURL,
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Limit error response size to prevent memory exhaustion from malicious servers
		body, _ := io.ReadAll(io.LimitReader(resp.Body, MaxErrorResponseSize))
		return nil, fmt.Errorf("refresh token failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	// Save the new token
	p.mu.Lock()
	newToken := &auth.Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		TokenType:    tokenResp.TokenType,
	}
	if err := p.tokenStore.Save("claude", p.tokenID, newToken); err != nil {
		p.mu.Unlock()
		return nil, fmt.Errorf("failed to save new token: %w", err)
	}
	p.mu.Unlock()

	return &tokenResp, nil
}

// SendRequest sends a synchronous request to Claude API
func (p *ClaudeProvider) SendRequest(ctx context.Context, req interface{}) (interface{}, error) {
	claudeReq, ok := req.(*ClaudeRequest)
	if !ok {
		return nil, fmt.Errorf("invalid request type: expected *ClaudeRequest")
	}

	// Ensure stream is false for sync requests
	claudeReq.Stream = false

	// Get valid token
	token, err := p.GetValidToken(ctx)
	if err != nil {
		return nil, err
	}

	// Build request
	body, err := json.Marshal(claudeReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	baseURL := util.NormalizeBaseURL(p.config.BaseURL)
	if baseURL == "" {
		baseURL = ClaudeAPIBaseURL
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	p.setClaudeHeaders(httpReq, token)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle error responses
	if resp.StatusCode != http.StatusOK {
		return nil, p.handleErrorResponse(resp)
	}

	var claudeResp ClaudeResponse
	if err := json.NewDecoder(resp.Body).Decode(&claudeResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &claudeResp, nil
}

// StreamRequest sends a streaming request to Claude API
func (p *ClaudeProvider) StreamRequest(ctx context.Context, req interface{}) (<-chan StreamEvent, error) {
	claudeReq, ok := req.(*ClaudeRequest)
	if !ok {
		return nil, fmt.Errorf("invalid request type: expected *ClaudeRequest")
	}

	// Ensure stream is true
	claudeReq.Stream = true

	// Get valid token
	token, err := p.GetValidToken(ctx)
	if err != nil {
		return nil, err
	}

	// Build request
	body, err := json.Marshal(claudeReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	baseURL := util.NormalizeBaseURL(p.config.BaseURL)
	if baseURL == "" {
		baseURL = ClaudeAPIBaseURL
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	p.setClaudeHeaders(httpReq, token)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, p.handleErrorResponse(resp)
	}

	eventCh := make(chan StreamEvent, 100)
	go p.processSSEStream(ctx, resp.Body, eventCh)

	return eventCh, nil
}

// setClaudeHeaders sets the required headers for Claude API requests
func (p *ClaudeProvider) setClaudeHeaders(req *http.Request, token string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-version", ClaudeAPIVersion)
	req.Header.Set("User-Agent", ClaudeUserAgent)
	req.Header.Set("X-Client-Type", "cli")

	// Add custom headers from config
	for k, v := range p.config.Headers {
		req.Header.Set(k, v)
	}
}

// handleErrorResponse handles error responses from Claude API
func (p *ClaudeProvider) handleErrorResponse(resp *http.Response) error {
	// Limit error response size to prevent memory exhaustion from malicious servers
	body, _ := io.ReadAll(io.LimitReader(resp.Body, MaxErrorResponseSize))

	var errResp struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return ErrAuthenticationFailed
		case http.StatusForbidden:
			return ErrAuthenticationFailed
		case http.StatusTooManyRequests:
			return ErrRateLimited
		default:
			return &ProviderError{
				Type:       errResp.Error.Type,
				Message:    errResp.Error.Message,
				StatusCode: resp.StatusCode,
				Retryable:  resp.StatusCode >= 500,
			}
		}
	}

	return fmt.Errorf("Claude API error (status %d): %s", resp.StatusCode, string(body))
}

// SSE Scanner buffer constants
const (
	// MaxSSEScanTokenSize is the maximum size for a single SSE line (1MB)
	// This prevents "token too long" errors for large model outputs
	MaxSSEScanTokenSize = 1024 * 1024
)

// processSSEStream processes Server-Sent Events from Claude
func (p *ClaudeProvider) processSSEStream(ctx context.Context, body io.ReadCloser, eventCh chan<- StreamEvent) {
	defer close(eventCh)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	// Set larger buffer to handle long SSE events (e.g., large code generation)
	// Default bufio.Scanner limit is 64KB which may be insufficient
	buf := make([]byte, MaxSSEScanTokenSize)
	scanner.Buffer(buf, MaxSSEScanTokenSize)

	var eventType string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			eventCh <- StreamEvent{Type: StreamEventTypeError, Error: ctx.Err()}
			return
		default:
		}

		line := scanner.Text()

		// Handle event type line
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		// Handle data line
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			// Parse and emit event based on type
			event := p.parseSSEEvent(eventType, []byte(data))
			if event.Type != StreamEventTypeDone || event.Error != nil {
				eventCh <- event
			}

			// Check for completion
			if eventType == "message_stop" {
				eventCh <- StreamEvent{Type: StreamEventTypeDone}
				return
			}
		}
	}

	if err := scanner.Err(); err != nil {
		eventCh <- StreamEvent{Type: StreamEventTypeError, Error: err}
	}
}

// parseSSEEvent parses a Claude SSE event
func (p *ClaudeProvider) parseSSEEvent(eventType string, data []byte) StreamEvent {
	switch eventType {
	case "message_start":
		var msg ClaudeMessageStart
		if err := json.Unmarshal(data, &msg); err != nil {
			return StreamEvent{Type: StreamEventTypeError, Error: err}
		}
		return StreamEvent{Type: StreamEventTypeData, Data: &msg, RawData: data}

	case "content_block_start":
		var block ClaudeContentBlockStart
		if err := json.Unmarshal(data, &block); err != nil {
			return StreamEvent{Type: StreamEventTypeError, Error: err}
		}
		// Check if it's a tool use block
		if block.ContentBlock.Type == "tool_use" {
			return StreamEvent{Type: StreamEventTypeToolCall, Data: &block, RawData: data}
		}
		return StreamEvent{Type: StreamEventTypeData, Data: &block, RawData: data}

	case "content_block_delta":
		var delta ClaudeContentBlockDelta
		if err := json.Unmarshal(data, &delta); err != nil {
			return StreamEvent{Type: StreamEventTypeError, Error: err}
		}
		// Check if it's a tool call delta
		if delta.Delta.Type == "input_json_delta" {
			return StreamEvent{Type: StreamEventTypeToolCall, Data: &delta, RawData: data}
		}
		return StreamEvent{Type: StreamEventTypeData, Data: &delta, RawData: data}

	case "content_block_stop":
		return StreamEvent{Type: StreamEventTypeData, RawData: data}

	case "message_delta":
		var delta ClaudeMessageDelta
		if err := json.Unmarshal(data, &delta); err != nil {
			return StreamEvent{Type: StreamEventTypeError, Error: err}
		}
		return StreamEvent{Type: StreamEventTypeData, Data: &delta, RawData: data}

	case "message_stop":
		return StreamEvent{Type: StreamEventTypeDone}

	case "error":
		var errData struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		json.Unmarshal(data, &errData)
		return StreamEvent{
			Type:  StreamEventTypeError,
			Error: fmt.Errorf("%s: %s", errData.Error.Type, errData.Error.Message),
		}

	default:
		return StreamEvent{Type: StreamEventTypeData, RawData: data}
	}
}

// Claude API Request/Response Types

// ClaudeRequest represents a Claude API request
type ClaudeRequest struct {
	Model         string          `json:"model"`
	System        string          `json:"system,omitempty"`
	Messages      []ClaudeMessage `json:"messages"`
	MaxTokens     int             `json:"max_tokens"`
	Temperature   *float64        `json:"temperature,omitempty"`
	TopP          *float64        `json:"top_p,omitempty"`
	TopK          *int            `json:"top_k,omitempty"`
	Stream        bool            `json:"stream,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	Tools         []ClaudeTool    `json:"tools,omitempty"`
	ToolChoice    interface{}     `json:"tool_choice,omitempty"`
	Metadata      *ClaudeMetadata `json:"metadata,omitempty"`
}

// ClaudeMessage represents a message in Claude format
type ClaudeMessage struct {
	Role    string      `json:"role"` // "user" or "assistant"
	Content interface{} `json:"content"` // string or []ClaudeContentBlock
}

// ClaudeContentBlock represents a content block in Claude format
type ClaudeContentBlock struct {
	Type      string          `json:"type"` // "text", "image", "tool_use", "tool_result"
	Text      string          `json:"text,omitempty"`
	Source    *ClaudeSource   `json:"source,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   interface{}     `json:"content,omitempty"` // for tool_result
	IsError   bool            `json:"is_error,omitempty"`
}

// ClaudeSource represents an image source
type ClaudeSource struct {
	Type      string `json:"type"` // "base64" or "url"
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

// ClaudeTool represents a tool definition
type ClaudeTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema interface{} `json:"input_schema"`
}

// ClaudeMetadata contains request metadata
type ClaudeMetadata struct {
	UserID string `json:"user_id,omitempty"`
}

// ClaudeResponse represents a Claude API response
type ClaudeResponse struct {
	ID           string               `json:"id"`
	Type         string               `json:"type"` // "message"
	Role         string               `json:"role"` // "assistant"
	Content      []ClaudeContentBlock `json:"content"`
	Model        string               `json:"model"`
	StopReason   string               `json:"stop_reason,omitempty"`
	StopSequence string               `json:"stop_sequence,omitempty"`
	Usage        ClaudeUsage          `json:"usage"`
}

// ClaudeUsage represents token usage
type ClaudeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Claude SSE Event Types

// ClaudeMessageStart is the message_start event data
type ClaudeMessageStart struct {
	Type    string `json:"type"`
	Message struct {
		ID           string        `json:"id"`
		Type         string        `json:"type"`
		Role         string        `json:"role"`
		Content      []interface{} `json:"content"`
		Model        string        `json:"model"`
		StopReason   *string       `json:"stop_reason"`
		StopSequence *string       `json:"stop_sequence"`
		Usage        ClaudeUsage   `json:"usage"`
	} `json:"message"`
}

// ClaudeContentBlockStart is the content_block_start event data
type ClaudeContentBlockStart struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock struct {
		Type  string `json:"type"` // "text" or "tool_use"
		Text  string `json:"text,omitempty"`
		ID    string `json:"id,omitempty"`
		Name  string `json:"name,omitempty"`
		Input struct{} `json:"input,omitempty"`
	} `json:"content_block"`
}

// ClaudeContentBlockDelta is the content_block_delta event data
type ClaudeContentBlockDelta struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type        string `json:"type"` // "text_delta" or "input_json_delta"
		Text        string `json:"text,omitempty"`
		PartialJSON string `json:"partial_json,omitempty"`
	} `json:"delta"`
}

// ClaudeMessageDelta is the message_delta event data
type ClaudeMessageDelta struct {
	Type  string `json:"type"`
	Delta struct {
		StopReason   string `json:"stop_reason,omitempty"`
		StopSequence string `json:"stop_sequence,omitempty"`
	} `json:"delta"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Ensure ClaudeProvider implements required interfaces
var (
	_ Provider      = (*ClaudeProvider)(nil)
	_ OAuthProvider = (*ClaudeProvider)(nil)
)

