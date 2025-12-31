package transformer

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/unifyai-proxy/unifyai-proxy/internal/provider"
)

// GeminiTransformer transforms between OpenAI and Gemini formats
type GeminiTransformer struct {
	defaultMaxTokens int
	// Stream context - set before processing stream events to ensure consistent metadata
	streamID    string // Stable ID for all chunks in a stream
	streamModel string // Model name for all chunks in a stream
}

// NewGeminiTransformer creates a new Gemini transformer
func NewGeminiTransformer() *GeminiTransformer {
	return &GeminiTransformer{
		defaultMaxTokens: 8192,
	}
}

// SetStreamContext sets the context for a streaming session.
// This must be called before processing stream events to ensure
// id and model are consistent across all chunks in the stream.
func (t *GeminiTransformer) SetStreamContext(model string) {
	t.streamID = t.generateID()
	t.streamModel = model
}

// ClearStreamContext clears the streaming context after a stream completes
func (t *GeminiTransformer) ClearStreamContext() {
	t.streamID = ""
	t.streamModel = ""
}

// ProviderType returns the target provider type
func (t *GeminiTransformer) ProviderType() string {
	return string(provider.ProviderTypeGemini)
}

// TransformRequest converts OpenAI request to Gemini format
func (t *GeminiTransformer) TransformRequest(req *OpenAIRequest) (interface{}, error) {
	if req == nil {
		return nil, ErrInvalidRequest
	}

	geminiReq := &provider.GeminiRequest{
		Model:    req.Model,
		Contents: make([]provider.GeminiContent, 0),
	}

	// Handle system message
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			geminiReq.SystemInstruction = &provider.GeminiContent{
				Parts: []provider.GeminiPart{{Text: msg.GetStringContent()}},
			}
			break
		}
	}

	// Convert messages
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			continue // Already handled
		}

		content := provider.GeminiContent{
			Role:  t.mapRole(msg.Role),
			Parts: t.convertContentToParts(&msg),
		}

		// Handle tool calls in assistant messages
		if len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				args := make(map[string]interface{})
				if tc.Function.Arguments != "" {
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
				}
				content.Parts = append(content.Parts, provider.GeminiPart{
					FunctionCall: &provider.GeminiFunctionCall{
						Name: tc.Function.Name,
						Args: args,
					},
				})
			}
		}

		// Handle tool result messages
		if msg.Role == "tool" && msg.ToolCallID != "" {
			content.Parts = []provider.GeminiPart{{
				FunctionResponse: &provider.GeminiFunctionResult{
					Name:     msg.Name,
					Response: map[string]interface{}{"result": msg.GetStringContent()},
				},
			}}
		}

		geminiReq.Contents = append(geminiReq.Contents, content)
	}

	// Convert tools
	if len(req.Tools) > 0 {
		tool := provider.GeminiTool{
			FunctionDeclarations: make([]provider.GeminiFunctionDecl, 0, len(req.Tools)),
		}
		for _, t := range req.Tools {
			if t.Type == "function" {
				tool.FunctionDeclarations = append(tool.FunctionDeclarations, provider.GeminiFunctionDecl{
					Name:        t.Function.Name,
					Description: t.Function.Description,
					Parameters:  t.Function.Parameters,
				})
			}
		}
		geminiReq.Tools = []provider.GeminiTool{tool}
	}

	// Generation config
	genConfig := &provider.GeminiGenerationConfig{}
	hasGenConfig := false

	if req.Temperature != nil {
		genConfig.Temperature = req.Temperature
		hasGenConfig = true
	}
	if req.TopP != nil {
		genConfig.TopP = req.TopP
		hasGenConfig = true
	}
	if req.MaxTokens != nil {
		genConfig.MaxOutputTokens = req.MaxTokens
		hasGenConfig = true
	}
	stopSeqs := t.convertStopSequences(req.Stop)
	if len(stopSeqs) > 0 {
		genConfig.StopSequences = stopSeqs
		hasGenConfig = true
	}

	if hasGenConfig {
		geminiReq.GenerationConfig = genConfig
	}

	return geminiReq, nil
}

// mapRole maps OpenAI roles to Gemini roles
func (t *GeminiTransformer) mapRole(role string) string {
	switch role {
	case "assistant":
		return "model"
	case "tool":
		return "user" // Tool results come from user side
	default:
		return role
	}
}

// convertStopSequences converts stop sequences to string slice
func (t *GeminiTransformer) convertStopSequences(stop interface{}) []string {
	if stop == nil {
		return nil
	}

	switch v := stop.(type) {
	case string:
		return []string{v}
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, s := range v {
			if str, ok := s.(string); ok {
				result = append(result, str)
			}
		}
		return result
	case []string:
		return v
	}
	return nil
}


// convertContentToParts converts message content to Gemini parts
func (t *GeminiTransformer) convertContentToParts(msg *OpenAIMessage) []provider.GeminiPart {
	parts := make([]provider.GeminiPart, 0)

	// Try to get string content first
	if text := msg.GetStringContent(); text != "" {
		parts = append(parts, provider.GeminiPart{Text: text})
		return parts
	}

	// Try to get content parts for multimodal
	contentParts, err := msg.GetContentParts()
	if err != nil || len(contentParts) == 0 {
		return parts
	}

	for _, cp := range contentParts {
		switch cp.Type {
		case "text":
			parts = append(parts, provider.GeminiPart{Text: cp.Text})
		case "image_url":
			if cp.ImageURL != nil {
				mimeType, data := t.parseDataURL(cp.ImageURL.URL)
				if data != "" {
					parts = append(parts, provider.GeminiPart{
						InlineData: &provider.GeminiInlineData{
							MimeType: mimeType,
							Data:     data,
						},
					})
				}
			}
		}
	}

	return parts
}

// parseDataURL parses a data URL and returns mime type and base64 data
func (t *GeminiTransformer) parseDataURL(url string) (string, string) {
	if !strings.HasPrefix(url, "data:") {
		return "", ""
	}

	// Format: data:mime/type;base64,<data>
	parts := strings.SplitN(url, ",", 2)
	if len(parts) != 2 {
		return "", ""
	}

	header := parts[0] // data:mime/type;base64
	data := parts[1]

	// Extract mime type
	header = strings.TrimPrefix(header, "data:")
	mimeType := strings.Split(header, ";")[0]

	return mimeType, data
}

// generateID generates a unique ID for responses
func (t *GeminiTransformer) generateID() string {
	return fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
}

// generateToolCallID generates a unique ID for tool calls
func (t *GeminiTransformer) generateToolCallID() string {
	return fmt.Sprintf("call_%d", time.Now().UnixNano())
}

// TransformResponse converts Gemini response to OpenAI format
func (t *GeminiTransformer) TransformResponse(resp interface{}) (*OpenAIResponse, error) {
	geminiResp, ok := resp.(*provider.GeminiResponse)
	if !ok {
		return nil, fmt.Errorf("invalid response type: expected *GeminiResponse")
	}

	openAIResp := &OpenAIResponse{
		ID:      t.generateID(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Choices: make([]Choice, 0),
	}

	for i, candidate := range geminiResp.Candidates {
		choice := Choice{
			Index:        i,
			FinishReason: t.mapFinishReason(candidate.FinishReason),
		}

		// Process content parts
		var textContent string
		var toolCalls []ToolCall

		toolCallIndex := 0
		for _, part := range candidate.Content.Parts {
			if part.Text != "" && !part.Thought {
				textContent += part.Text
			} else if part.FunctionCall != nil {
				argsJSON, _ := json.Marshal(part.FunctionCall.Args)
				idx := toolCallIndex
				toolCalls = append(toolCalls, ToolCall{
					Index: &idx,
					ID:    t.generateToolCallID(),
					Type:  "function",
					Function: FunctionCall{
						Name:      part.FunctionCall.Name,
						Arguments: string(argsJSON),
					},
				})
				toolCallIndex++
			}
		}

		choice.Message = &OpenAIMessage{
			Role:      "assistant",
			Content:   textContent,
			ToolCalls: toolCalls,
		}

		openAIResp.Choices = append(openAIResp.Choices, choice)
	}

	// Convert usage
	if geminiResp.UsageMetadata != nil {
		openAIResp.Usage = &OpenAIUsage{
			PromptTokens:     geminiResp.UsageMetadata.PromptTokenCount,
			CompletionTokens: geminiResp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      geminiResp.UsageMetadata.TotalTokenCount,
		}
	}

	return openAIResp, nil
}

// mapFinishReason maps Gemini finish reasons to OpenAI format
func (t *GeminiTransformer) mapFinishReason(reason string) string {
	switch reason {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY":
		return "content_filter"
	case "RECITATION":
		return "content_filter"
	case "OTHER":
		return "stop"
	default:
		return "stop"
	}
}

// TransformStreamEvent converts Gemini stream events to OpenAI format
func (t *GeminiTransformer) TransformStreamEvent(event interface{}) (*OpenAIStreamChunk, error) {
	geminiEvent, ok := event.(*provider.GeminiResponse)
	if !ok {
		return nil, fmt.Errorf("invalid event type: expected *GeminiResponse")
	}

	// Use stable stream ID and model if set via SetStreamContext,
	// otherwise fall back to generating new values (non-ideal but functional)
	chunkID := t.streamID
	if chunkID == "" {
		chunkID = t.generateID()
	}
	chunkModel := t.streamModel
	// If model not set via context, try to get from response (Gemini doesn't typically include this)
	if chunkModel == "" && geminiEvent.ModelVersion != "" {
		chunkModel = geminiEvent.ModelVersion
	}

	chunk := &OpenAIStreamChunk{
		ID:      chunkID,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   chunkModel,
		Choices: make([]Choice, 0),
	}

	for i, candidate := range geminiEvent.Candidates {
		choice := Choice{
			Index: i,
			Delta: &OpenAIMessage{},
		}

		toolCallIndex := 0
		for _, part := range candidate.Content.Parts {
			if part.Text != "" && !part.Thought {
				choice.Delta.Content = part.Text
			} else if part.FunctionCall != nil {
				argsJSON, _ := json.Marshal(part.FunctionCall.Args)
				idx := toolCallIndex
				choice.Delta.ToolCalls = append(choice.Delta.ToolCalls, ToolCall{
					Index: &idx,
					ID:    t.generateToolCallID(),
					Type:  "function",
					Function: FunctionCall{
						Name:      part.FunctionCall.Name,
						Arguments: string(argsJSON),
					},
				})
				toolCallIndex++
			}
		}

		if candidate.FinishReason != "" {
			choice.FinishReason = t.mapFinishReason(candidate.FinishReason)
		}

		chunk.Choices = append(chunk.Choices, choice)
	}

	return chunk, nil
}

// Ensure GeminiTransformer implements Transformer interface
var _ Transformer = (*GeminiTransformer)(nil)

// Ensure GeminiTransformer implements StreamContextSetter interface
var _ StreamContextSetter = (*GeminiTransformer)(nil)

// Unused import guards
var (
	_ = base64.StdEncoding
	_ = strings.HasPrefix
)
