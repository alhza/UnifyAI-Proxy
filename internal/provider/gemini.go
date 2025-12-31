package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/unifyai-proxy/unifyai-proxy/internal/auth"
	"github.com/unifyai-proxy/unifyai-proxy/internal/util"
)

// Gemini API constants
const (
	GeminiAPIBaseURL   = "https://generativelanguage.googleapis.com"
	GeminiOAuthBaseURL = "https://accounts.google.com"
	GeminiClientID     = "667607542161-gua21ma1uangl0mphq8rm62rpi1klh6h.apps.googleusercontent.com"
	GeminiCallbackPort = 54546
	GeminiUserAgent    = "gemini-cli/1.0.0"
	GeminiAPIVersion   = "v1beta"
)

// GeminiProvider implements the Provider interface for Google Gemini
type GeminiProvider struct {
	config     ProviderConfig
	oauthCfg   OAuthConfig
	httpClient *http.Client
	tokenStore auth.Store
	tokenID    string
	mu         sync.RWMutex
}

// NewGeminiProvider creates a new Gemini provider instance
func NewGeminiProvider(config ProviderConfig, tokenStore auth.Store, tokenID string) *GeminiProvider {
	client := &http.Client{
		Timeout: time.Duration(config.Timeout) * time.Second,
	}

	if config.ProxyURL != "" {
		proxyURL, err := url.Parse(config.ProxyURL)
		if err == nil {
			client.Transport = &http.Transport{
				Proxy: http.ProxyURL(proxyURL),
			}
		}
	}

	return &GeminiProvider{
		config:     config,
		oauthCfg:   GeminiOAuthConfigDefault(),
		httpClient: client,
		tokenStore: tokenStore,
		tokenID:    tokenID,
	}
}

// GeminiOAuthConfigDefault returns the default OAuth config for Gemini
func GeminiOAuthConfigDefault() OAuthConfig {
	return OAuthConfig{
		CallbackPort:     GeminiCallbackPort,
		CallbackPortType: "dynamic",
		RequiresPKCE:     true,
		ClientID:         GeminiClientID,
		Scopes:           []string{"https://www.googleapis.com/auth/cloud-platform", "https://www.googleapis.com/auth/generative-language"},
		AuthorizeURL:     GeminiOAuthBaseURL + "/o/oauth2/v2/auth",
		TokenURL:         GeminiOAuthBaseURL + "/o/oauth2/token",
		RedirectURI:      fmt.Sprintf("http://localhost:%d/callback", GeminiCallbackPort),
	}
}

// Name returns the provider name
func (p *GeminiProvider) Name() string {
	return string(ProviderTypeGemini)
}

// Capabilities returns the supported capabilities
func (p *GeminiProvider) Capabilities() []Capability {
	return []Capability{
		CapabilityText,
		CapabilityMultimodal,
		CapabilityToolCalling,
		CapabilityVision,
		CapabilityStreaming,
		CapabilityThinking,
		CapabilityCodeExecution,
	}
}

// SupportedModels returns the list of supported Gemini models
func (p *GeminiProvider) SupportedModels() []string {
	if len(p.config.Models) > 0 {
		return p.config.Models
	}
	return []string{
		"gemini-2.5-pro",
		"gemini-2.5-flash",
		"gemini-2.0-flash",
		"gemini-2.0-flash-thinking-exp",
		"gemini-1.5-pro",
		"gemini-1.5-flash",
	}
}

// SupportsModel checks if a specific model is supported
func (p *GeminiProvider) SupportsModel(model string) bool {
	for _, excluded := range p.config.ExcludedModels {
		if excluded == model || util.MatchWildcard(excluded, model) {
			return false
		}
	}

	if len(p.config.Models) > 0 {
		for _, m := range p.config.Models {
			if m == model || util.MatchWildcard(m, model) {
				return true
			}
		}
		return false
	}

	return strings.HasPrefix(model, "gemini-")
}

// OAuthConfig returns the OAuth configuration
func (p *GeminiProvider) OAuthConfig() OAuthConfig {
	return p.oauthCfg
}

// GetValidToken returns a valid access token, refreshing if necessary
func (p *GeminiProvider) GetValidToken(ctx context.Context) (string, error) {
	p.mu.RLock()
	token, err := p.tokenStore.Load("gemini", p.tokenID)
	p.mu.RUnlock()

	if err != nil {
		return "", fmt.Errorf("failed to load token: %w", err)
	}

	if token.IsValid() {
		return token.AccessToken, nil
	}

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
func (p *GeminiProvider) RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
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
		body, _ := io.ReadAll(resp.Body)
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
	if newToken.RefreshToken == "" {
		newToken.RefreshToken = refreshToken // Keep old refresh token if not returned
	}
	if err := p.tokenStore.Save("gemini", p.tokenID, newToken); err != nil {
		p.mu.Unlock()
		return nil, fmt.Errorf("failed to save new token: %w", err)
	}
	p.mu.Unlock()

	return &tokenResp, nil
}

// SendRequest sends a synchronous request to Gemini API
func (p *GeminiProvider) SendRequest(ctx context.Context, req interface{}) (interface{}, error) {
	geminiReq, ok := req.(*GeminiRequest)
	if !ok {
		return nil, fmt.Errorf("invalid request type: expected *GeminiRequest")
	}

	token, err := p.GetValidToken(ctx)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	baseURL := p.config.BaseURL
	if baseURL == "" {
		baseURL = GeminiAPIBaseURL
	}

	endpoint := fmt.Sprintf("%s/%s/models/%s:generateContent", baseURL, GeminiAPIVersion, geminiReq.Model)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	p.setGeminiHeaders(httpReq, token)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, p.handleErrorResponse(resp)
	}

	var geminiResp GeminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &geminiResp, nil
}

// StreamRequest sends a streaming request to Gemini API
func (p *GeminiProvider) StreamRequest(ctx context.Context, req interface{}) (<-chan StreamEvent, error) {
	geminiReq, ok := req.(*GeminiRequest)
	if !ok {
		return nil, fmt.Errorf("invalid request type: expected *GeminiRequest")
	}

	token, err := p.GetValidToken(ctx)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	baseURL := p.config.BaseURL
	if baseURL == "" {
		baseURL = GeminiAPIBaseURL
	}

	endpoint := fmt.Sprintf("%s/%s/models/%s:streamGenerateContent?alt=sse", baseURL, GeminiAPIVersion, geminiReq.Model)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	p.setGeminiHeaders(httpReq, token)

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

// setGeminiHeaders sets the required headers for Gemini API requests
func (p *GeminiProvider) setGeminiHeaders(req *http.Request, token string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", GeminiUserAgent)

	for k, v := range p.config.Headers {
		req.Header.Set(k, v)
	}
}

// handleErrorResponse handles error responses from Gemini API
func (p *GeminiProvider) handleErrorResponse(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	var errResp struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
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
				Type:       errResp.Error.Status,
				Message:    errResp.Error.Message,
				StatusCode: resp.StatusCode,
				Retryable:  resp.StatusCode >= 500,
			}
		}
	}

	return fmt.Errorf("Gemini API error (status %d): %s", resp.StatusCode, string(body))
}

// processSSEStream processes Server-Sent Events from Gemini
func (p *GeminiProvider) processSSEStream(ctx context.Context, body io.ReadCloser, eventCh chan<- StreamEvent) {
	defer close(eventCh)
	defer body.Close()

	scanner := bufio.NewScanner(body)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			eventCh <- StreamEvent{Type: StreamEventTypeError, Error: ctx.Err()}
			return
		default:
		}

		line := scanner.Text()

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			var resp GeminiStreamResponse
			if err := json.Unmarshal([]byte(data), &resp); err != nil {
				eventCh <- StreamEvent{Type: StreamEventTypeError, Error: err}
				continue
			}

			// Check for thinking content
			eventType := StreamEventTypeData
			if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
				part := resp.Candidates[0].Content.Parts[0]
				if part.Thought {
					eventType = StreamEventTypeThinking
				}
			}

			eventCh <- StreamEvent{
				Type:    eventType,
				Data:    &resp,
				RawData: []byte(data),
			}

			// Check for finish reason
			if len(resp.Candidates) > 0 && resp.Candidates[0].FinishReason != "" {
				eventCh <- StreamEvent{Type: StreamEventTypeDone}
				return
			}
		}
	}

	if err := scanner.Err(); err != nil {
		eventCh <- StreamEvent{Type: StreamEventTypeError, Error: err}
	}
}

// Gemini API Request/Response Types

// GeminiRequest represents a Gemini API request
type GeminiRequest struct {
	Model             string                   `json:"-"` // Used in URL, not body
	Contents          []GeminiContent          `json:"contents"`
	SystemInstruction *GeminiContent           `json:"systemInstruction,omitempty"`
	Tools             []GeminiTool             `json:"tools,omitempty"`
	ToolConfig        *GeminiToolConfig        `json:"toolConfig,omitempty"`
	GenerationConfig  *GeminiGenerationConfig  `json:"generationConfig,omitempty"`
	SafetySettings    []GeminiSafetySetting    `json:"safetySettings,omitempty"`
}

// GeminiContent represents content in Gemini format
type GeminiContent struct {
	Role  string       `json:"role,omitempty"` // "user" or "model"
	Parts []GeminiPart `json:"parts"`
}

// GeminiPart represents a part of content
type GeminiPart struct {
	Text             string                `json:"text,omitempty"`
	InlineData       *GeminiInlineData     `json:"inlineData,omitempty"`
	FunctionCall     *GeminiFunctionCall   `json:"functionCall,omitempty"`
	FunctionResponse *GeminiFunctionResult `json:"functionResponse,omitempty"`
	Thought          bool                  `json:"thought,omitempty"`
}

// GeminiInlineData represents inline data (images, etc.)
type GeminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"` // base64 encoded
}

// GeminiFunctionCall represents a function call from the model
type GeminiFunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

// GeminiFunctionResult represents a function result
type GeminiFunctionResult struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

// GeminiTool represents a tool definition
type GeminiTool struct {
	FunctionDeclarations []GeminiFunctionDecl `json:"functionDeclarations,omitempty"`
	CodeExecution        *struct{}            `json:"codeExecution,omitempty"`
}

// GeminiFunctionDecl represents a function declaration
type GeminiFunctionDecl struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

// GeminiToolConfig represents tool configuration
type GeminiToolConfig struct {
	FunctionCallingConfig *GeminiFunctionCallingConfig `json:"functionCallingConfig,omitempty"`
}

// GeminiFunctionCallingConfig configures function calling behavior
type GeminiFunctionCallingConfig struct {
	Mode                 string   `json:"mode,omitempty"` // AUTO, ANY, NONE
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

// GeminiGenerationConfig configures generation parameters
type GeminiGenerationConfig struct {
	Temperature       *float64 `json:"temperature,omitempty"`
	TopP              *float64 `json:"topP,omitempty"`
	TopK              *int     `json:"topK,omitempty"`
	MaxOutputTokens   *int     `json:"maxOutputTokens,omitempty"`
	StopSequences     []string `json:"stopSequences,omitempty"`
	ResponseMimeType  string   `json:"responseMimeType,omitempty"`
	ResponseSchema    interface{} `json:"responseSchema,omitempty"`
	ThinkingConfig    *GeminiThinkingConfig `json:"thinkingConfig,omitempty"`
}

// GeminiThinkingConfig configures thinking mode
type GeminiThinkingConfig struct {
	ThinkingBudget int `json:"thinkingBudget,omitempty"`
}

// GeminiSafetySetting represents a safety setting
type GeminiSafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

// GeminiResponse represents a Gemini API response
type GeminiResponse struct {
	Candidates     []GeminiCandidate    `json:"candidates"`
	UsageMetadata  *GeminiUsageMetadata `json:"usageMetadata,omitempty"`
	ModelVersion   string               `json:"modelVersion,omitempty"`
}

// GeminiStreamResponse is the same as GeminiResponse for streaming
type GeminiStreamResponse = GeminiResponse

// GeminiCandidate represents a response candidate
type GeminiCandidate struct {
	Content       GeminiContent        `json:"content"`
	FinishReason  string               `json:"finishReason,omitempty"`
	SafetyRatings []GeminiSafetyRating `json:"safetyRatings,omitempty"`
	Index         int                  `json:"index,omitempty"`
}

// GeminiSafetyRating represents a safety rating
type GeminiSafetyRating struct {
	Category    string `json:"category"`
	Probability string `json:"probability"`
}

// GeminiUsageMetadata represents token usage
type GeminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
	ThoughtsTokenCount   int `json:"thoughtsTokenCount,omitempty"`
}

// Ensure GeminiProvider implements required interfaces
var (
	_ Provider      = (*GeminiProvider)(nil)
	_ OAuthProvider = (*GeminiProvider)(nil)
)

// Unused import guards
var (
	_ = bufio.NewReader
	_ = bytes.NewReader
)

