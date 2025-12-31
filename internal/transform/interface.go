package transform

import (
	"sync"
	"time"

	"github.com/unifyai-proxy/unifyai-proxy/internal/account"
	"github.com/unifyai-proxy/unifyai-proxy/internal/provider"
)

// RouteCtx contains routing context for request transformation
// Reference: docs/unifyai_proxy/15-任务拆解-基础设计.md
type RouteCtx struct {
	// Model is the requested model name
	Model string
	// Provider is the target provider (claude/gemini/codex)
	Provider string
	// Account is the selected account
	Account *account.Account
	// Capabilities are the required capabilities (uses provider.Capability for consistency)
	Capabilities []provider.Capability
	// ProxyURL is the proxy configuration
	ProxyURL string
	// Timeout is the request timeout
	Timeout time.Duration
	// Metadata contains extension metadata
	Metadata map[string]interface{}
	// Stream indicates if streaming is requested
	Stream bool
	// RequestID is a unique request identifier
	RequestID string
}

// Transformer defines the interface for request/response transformation
// Reference: docs/unifyai_proxy/15-任务拆解-基础设计.md
type Transformer interface {
	// Name returns the transformer name (matches provider name)
	Name() string
	// TransformRequest transforms an OpenAI request to provider-specific format
	TransformRequest(req *OpenAIRequest, ctx RouteCtx) (interface{}, error)
	// TransformResponse transforms a provider response to OpenAI format
	TransformResponse(resp interface{}) (*OpenAIResponse, error)
	// TransformStreamEvent transforms a streaming event to OpenAI format
	TransformStreamEvent(event interface{}) (*OpenAIStreamChunk, error)
}

// OpenAI Request/Response Types

// OpenAIRequest represents an OpenAI-compatible chat completion request
type OpenAIRequest struct {
	Model            string                 `json:"model"`
	Messages         []OpenAIMessage        `json:"messages"`
	Temperature      *float64               `json:"temperature,omitempty"`
	TopP             *float64               `json:"top_p,omitempty"`
	N                *int                   `json:"n,omitempty"`
	Stream           bool                   `json:"stream,omitempty"`
	Stop             interface{}            `json:"stop,omitempty"` // string or []string
	MaxTokens        *int                   `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int                `json:"max_completion_tokens,omitempty"`
	PresencePenalty  *float64               `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64               `json:"frequency_penalty,omitempty"`
	LogitBias        map[string]float64     `json:"logit_bias,omitempty"`
	User             string                 `json:"user,omitempty"`
	Tools            []OpenAITool           `json:"tools,omitempty"`
	ToolChoice       interface{}            `json:"tool_choice,omitempty"` // "none", "auto", or object
	ResponseFormat   *OpenAIResponseFormat  `json:"response_format,omitempty"`
	Seed             *int                   `json:"seed,omitempty"`
	StreamOptions    *StreamOptions         `json:"stream_options,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// OpenAIMessage represents a message in the conversation
type OpenAIMessage struct {
	Role       string            `json:"role"` // system, user, assistant, tool
	Content    interface{}       `json:"content"` // string or []ContentPart
	Name       string            `json:"name,omitempty"`
	ToolCalls  []OpenAIToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
}

// ContentPart represents a part of multimodal content
type ContentPart struct {
	Type     string    `json:"type"` // "text" or "image_url"
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL represents an image URL in content
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // "low", "high", "auto"
}

// OpenAITool represents a tool/function definition
type OpenAITool struct {
	Type     string           `json:"type"` // "function"
	Function OpenAIFunction   `json:"function"`
}

// OpenAIFunction represents a function definition
type OpenAIFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"` // JSON Schema
	Strict      *bool       `json:"strict,omitempty"`
}

// OpenAIToolCall represents a tool call from the assistant
type OpenAIToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"` // "function"
	Function OpenAIFunctionCall   `json:"function"`
	Index    *int                 `json:"index,omitempty"` // for streaming
}

// OpenAIFunctionCall represents a function call
type OpenAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// OpenAIResponseFormat specifies the response format
type OpenAIResponseFormat struct {
	Type       string      `json:"type"` // "text", "json_object", "json_schema"
	JSONSchema interface{} `json:"json_schema,omitempty"`
}

// StreamOptions specifies streaming options
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// OpenAIResponse represents an OpenAI-compatible chat completion response
type OpenAIResponse struct {
	ID                string         `json:"id"`
	Object            string         `json:"object"` // "chat.completion"
	Created           int64          `json:"created"`
	Model             string         `json:"model"`
	Choices           []OpenAIChoice `json:"choices"`
	Usage             *OpenAIUsage   `json:"usage,omitempty"`
	SystemFingerprint string         `json:"system_fingerprint,omitempty"`
}


// OpenAIChoice represents a completion choice
type OpenAIChoice struct {
	Index        int            `json:"index"`
	Message      *OpenAIMessage `json:"message,omitempty"`
	Delta        *OpenAIDelta   `json:"delta,omitempty"` // for streaming
	FinishReason string         `json:"finish_reason,omitempty"`
	Logprobs     interface{}    `json:"logprobs,omitempty"`
}

// OpenAIDelta represents incremental content in streaming
type OpenAIDelta struct {
	Role      string           `json:"role,omitempty"`
	Content   string           `json:"content,omitempty"`
	ToolCalls []OpenAIToolCall `json:"tool_calls,omitempty"`
}

// OpenAIUsage represents token usage information
type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// OpenAIStreamChunk represents a streaming chunk response
type OpenAIStreamChunk struct {
	ID                string         `json:"id"`
	Object            string         `json:"object"` // "chat.completion.chunk"
	Created           int64          `json:"created"`
	Model             string         `json:"model"`
	Choices           []OpenAIChoice `json:"choices"`
	Usage             *OpenAIUsage   `json:"usage,omitempty"` // only with include_usage
	SystemFingerprint string         `json:"system_fingerprint,omitempty"`
}

// OpenAIError represents an OpenAI-compatible error response
type OpenAIError struct {
	Error OpenAIErrorDetail `json:"error"`
}

// OpenAIErrorDetail contains error details
type OpenAIErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param,omitempty"`
	Code    string `json:"code,omitempty"`
}

// TransformService manages transformers for different providers
type TransformService struct {
	transformers map[string]Transformer
	mu           sync.RWMutex
}

// NewTransformService creates a new transform service
func NewTransformService() *TransformService {
	return &TransformService{
		transformers: make(map[string]Transformer),
	}
}

// Register registers a transformer for a provider
func (s *TransformService) Register(t Transformer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transformers[t.Name()] = t
}

// Unregister removes a transformer by provider name
func (s *TransformService) Unregister(providerName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.transformers, providerName)
}

// GetTransformer returns the transformer for a provider
func (s *TransformService) GetTransformer(providerName string) Transformer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.transformers[providerName]
}

// HasTransformer checks if a transformer is registered for a provider
func (s *TransformService) HasTransformer(providerName string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.transformers[providerName]
	return exists
}

// ListTransformers returns all registered transformer names
func (s *TransformService) ListTransformers() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.transformers))
	for name := range s.transformers {
		names = append(names, name)
	}
	return names
}

// Count returns the number of registered transformers
func (s *TransformService) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.transformers)
}

// Helper functions for request analysis

// GetRequestedCapabilities analyzes a request and returns required capabilities
func GetRequestedCapabilities(req *OpenAIRequest) []provider.Capability {
	caps := []provider.Capability{provider.CapabilityText}

	if req.Stream {
		caps = append(caps, provider.CapabilityStreaming)
	}

	// Check for tool calling
	if len(req.Tools) > 0 {
		caps = append(caps, provider.CapabilityToolCalling)
	}

	// Check for multimodal content
	if hasMultimodalContent(req.Messages) {
		caps = append(caps, provider.CapabilityMultimodal, provider.CapabilityVision)
	}

	return caps
}

// hasMultimodalContent checks if messages contain image content
func hasMultimodalContent(messages []OpenAIMessage) bool {
	for _, msg := range messages {
		switch content := msg.Content.(type) {
		case []interface{}:
			for _, part := range content {
				if partMap, ok := part.(map[string]interface{}); ok {
					if partMap["type"] == "image_url" {
						return true
					}
				}
			}
		case []ContentPart:
			for _, part := range content {
				if part.Type == "image_url" {
					return true
				}
			}
		}
	}
	return false
}

// ExtractSystemMessage extracts system message from messages array
func ExtractSystemMessage(messages []OpenAIMessage) (string, []OpenAIMessage) {
	var systemContent string
	var otherMessages []OpenAIMessage

	for _, msg := range messages {
		if msg.Role == "system" {
			if content, ok := msg.Content.(string); ok {
				if systemContent != "" {
					systemContent += "\n"
				}
				systemContent += content
			}
		} else {
			otherMessages = append(otherMessages, msg)
		}
	}

	return systemContent, otherMessages
}

// ValidateRequest validates an OpenAI request
func ValidateRequest(req *OpenAIRequest) error {
	if req.Model == "" {
		return ErrMissingModel
	}
	if len(req.Messages) == 0 {
		return ErrMissingMessages
	}
	return nil
}

// Common transform errors
var (
	ErrMissingModel      = &TransformError{Code: "missing_model", Message: "model is required"}
	ErrMissingMessages   = &TransformError{Code: "missing_messages", Message: "messages are required"}
	ErrInvalidContent    = &TransformError{Code: "invalid_content", Message: "invalid message content"}
	ErrUnsupportedFormat = &TransformError{Code: "unsupported_format", Message: "unsupported response format"}
)

// TransformError represents a transformation error
type TransformError struct {
	Code    string
	Message string
}

func (e *TransformError) Error() string {
	return e.Message
}
