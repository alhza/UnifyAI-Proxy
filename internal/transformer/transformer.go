package transformer

import (
	"encoding/json"
	"errors"
)

// Transformer defines the interface for request/response transformation
// Transforms OpenAI-compatible format to/from provider-specific formats
type Transformer interface {
	// TransformRequest transforms OpenAI request to provider-specific format
	TransformRequest(req *OpenAIRequest) (interface{}, error)
	// TransformResponse transforms provider-specific response to OpenAI format
	TransformResponse(resp interface{}) (*OpenAIResponse, error)
	// TransformStreamEvent transforms provider-specific stream event to OpenAI format
	TransformStreamEvent(event interface{}) (*OpenAIStreamChunk, error)
	// ProviderType returns the target provider type
	ProviderType() string
}

// StreamContextSetter is an optional interface for transformers that need
// to maintain state across stream events (e.g., stable ID and model name).
// Transformers that implement this interface will have SetStreamContext called
// before streaming begins and ClearStreamContext called after streaming ends.
type StreamContextSetter interface {
	// SetStreamContext initializes the streaming context with the model name.
	// This ensures consistent id and model values across all stream chunks.
	SetStreamContext(model string)
	// ClearStreamContext clears the streaming context after a stream completes.
	ClearStreamContext()
}

// Common errors
var (
	ErrInvalidRequest       = errors.New("invalid request format")
	ErrInvalidResponse      = errors.New("invalid response format")
	ErrUnsupportedFeature   = errors.New("unsupported feature")
	ErrInvalidMessageFormat = errors.New("invalid message format")
	ErrInvalidContentType   = errors.New("invalid content type")
)

// OpenAI-compatible request/response types

// OpenAIRequest represents an OpenAI-compatible chat completion request
type OpenAIRequest struct {
	Model            string           `json:"model"`
	Messages         []OpenAIMessage  `json:"messages"`
	MaxTokens        *int             `json:"max_tokens,omitempty"`
	Temperature      *float64         `json:"temperature,omitempty"`
	TopP             *float64         `json:"top_p,omitempty"`
	N                *int             `json:"n,omitempty"`
	Stream           bool             `json:"stream,omitempty"`
	Stop             interface{}      `json:"stop,omitempty"`
	PresencePenalty  *float64         `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64         `json:"frequency_penalty,omitempty"`
	LogitBias        map[string]int   `json:"logit_bias,omitempty"`
	User             string           `json:"user,omitempty"`
	Tools            []OpenAITool     `json:"tools,omitempty"`
	ToolChoice       interface{}      `json:"tool_choice,omitempty"`
	ResponseFormat   *ResponseFormat  `json:"response_format,omitempty"`
	Seed             *int             `json:"seed,omitempty"`
	StreamOptions    *StreamOptions   `json:"stream_options,omitempty"`
}

// OpenAIMessage represents a message in OpenAI format
type OpenAIMessage struct {
	Role       string      `json:"role"` // "system", "user", "assistant", "tool"
	Content    interface{} `json:"content"` // string or []ContentPart
	Name       string      `json:"name,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
}

// ContentPart represents a part of multimodal content
type ContentPart struct {
	Type     string    `json:"type"` // "text", "image_url"
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL represents an image URL reference
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // "low", "high", "auto"
}

// OpenAITool represents a tool definition
type OpenAITool struct {
	Type     string       `json:"type"` // "function"
	Function FunctionDef  `json:"function"`
}

// FunctionDef represents a function definition
type FunctionDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"`
	Strict      *bool       `json:"strict,omitempty"`
}

// ToolCall represents a tool call from the assistant
type ToolCall struct {
	Index    *int         `json:"index,omitempty"`    // Required for streaming deltas to identify which tool call
	ID       string       `json:"id,omitempty"`       // Tool call ID (first chunk only in streaming)
	Type     string       `json:"type,omitempty"`     // "function" (first chunk only in streaming)
	Function FunctionCall `json:"function"`
}

// FunctionCall represents a function call
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ResponseFormat specifies the output format
type ResponseFormat struct {
	Type       string      `json:"type"` // "text", "json_object", "json_schema"
	JSONSchema interface{} `json:"json_schema,omitempty"`
}

// StreamOptions contains stream-specific options
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// OpenAIResponse represents an OpenAI chat completion response
type OpenAIResponse struct {
	ID                string         `json:"id"`
	Object            string         `json:"object"` // "chat.completion"
	Created           int64          `json:"created"`
	Model             string         `json:"model"`
	Choices           []Choice       `json:"choices"`
	Usage             *OpenAIUsage   `json:"usage,omitempty"`
	SystemFingerprint string         `json:"system_fingerprint,omitempty"`
}

// Choice represents a completion choice
type Choice struct {
	Index        int            `json:"index"`
	Message      *OpenAIMessage `json:"message,omitempty"`
	Delta        *OpenAIMessage `json:"delta,omitempty"`
	FinishReason string         `json:"finish_reason,omitempty"`
	Logprobs     interface{}    `json:"logprobs,omitempty"`
}

// OpenAIUsage represents token usage
type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// OpenAIStreamChunk represents a streaming response chunk
type OpenAIStreamChunk struct {
	ID                string       `json:"id"`
	Object            string       `json:"object"` // "chat.completion.chunk"
	Created           int64        `json:"created"`
	Model             string       `json:"model"`
	Choices           []Choice     `json:"choices"`
	Usage             *OpenAIUsage `json:"usage,omitempty"`
	SystemFingerprint string       `json:"system_fingerprint,omitempty"`
}

// GetStringContent extracts string content from a message
func (m *OpenAIMessage) GetStringContent() string {
	if s, ok := m.Content.(string); ok {
		return s
	}
	return ""
}

// GetContentParts extracts content parts from a message
func (m *OpenAIMessage) GetContentParts() ([]ContentPart, error) {
	if m.Content == nil {
		return nil, nil
	}

	// If it's already a string, wrap in a text part
	if s, ok := m.Content.(string); ok {
		return []ContentPart{{Type: "text", Text: s}}, nil
	}

	// Try to unmarshal as []ContentPart
	data, err := json.Marshal(m.Content)
	if err != nil {
		return nil, err
	}

	var parts []ContentPart
	if err := json.Unmarshal(data, &parts); err != nil {
		return nil, ErrInvalidContentType
	}

	return parts, nil
}

