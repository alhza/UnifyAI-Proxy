package transformer

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/unifyai-proxy/unifyai-proxy/internal/provider"
)

// ClaudeTransformer transforms OpenAI format to/from Claude format
type ClaudeTransformer struct {
	defaultMaxTokens int
	// Stream context - ensures consistent id and model across all chunks in a stream
	streamID      string
	streamModel   string
	streamCreated int64
	mu            sync.Mutex
}

// NewClaudeTransformer creates a new Claude transformer
func NewClaudeTransformer() *ClaudeTransformer {
	return &ClaudeTransformer{
		defaultMaxTokens: provider.DefaultMaxTokens,
	}
}

// SetStreamContext sets the context for a streaming session.
// This ensures id and model are consistent across all chunks in the stream.
func (t *ClaudeTransformer) SetStreamContext(model string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.streamID = fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	t.streamModel = model
	t.streamCreated = time.Now().Unix()
}

// ClearStreamContext clears the streaming context after a stream completes
func (t *ClaudeTransformer) ClearStreamContext() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.streamID = ""
	t.streamModel = ""
	t.streamCreated = 0
}

// getStreamContext returns the current stream context (thread-safe)
func (t *ClaudeTransformer) getStreamContext() (string, string, int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.streamID, t.streamModel, t.streamCreated
}

// ProviderType returns the target provider type
func (t *ClaudeTransformer) ProviderType() string {
	return string(provider.ProviderTypeClaude)
}

// TransformRequest transforms OpenAI request to Claude format
func (t *ClaudeTransformer) TransformRequest(req *OpenAIRequest) (interface{}, error) {
	if req == nil {
		return nil, ErrInvalidRequest
	}

	claudeReq := &provider.ClaudeRequest{
		Model:  req.Model,
		Stream: req.Stream,
	}

	// Set max tokens
	if req.MaxTokens != nil {
		claudeReq.MaxTokens = *req.MaxTokens
	} else {
		claudeReq.MaxTokens = t.defaultMaxTokens
	}

	// Copy optional parameters
	claudeReq.Temperature = req.Temperature
	claudeReq.TopP = req.TopP

	// Convert stop sequences
	if req.Stop != nil {
		claudeReq.StopSequences = t.convertStopSequences(req.Stop)
	}

	// Convert messages
	system, messages, err := t.convertMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	claudeReq.System = system
	claudeReq.Messages = messages

	// Convert tools
	if len(req.Tools) > 0 {
		claudeReq.Tools = t.convertTools(req.Tools)
		claudeReq.ToolChoice = t.convertToolChoice(req.ToolChoice)
	}

	// Set metadata
	if req.User != "" {
		claudeReq.Metadata = &provider.ClaudeMetadata{
			UserID: req.User,
		}
	}

	return claudeReq, nil
}

// convertMessages converts OpenAI messages to Claude format
func (t *ClaudeTransformer) convertMessages(msgs []OpenAIMessage) (string, []provider.ClaudeMessage, error) {
	var systemPrompt string
	var claudeMsgs []provider.ClaudeMessage

	for _, msg := range msgs {
		switch msg.Role {
		case "system":
			// Claude handles system prompt separately
			if s, ok := msg.Content.(string); ok {
				if systemPrompt != "" {
					systemPrompt += "\n\n"
				}
				systemPrompt += s
			}

		case "user":
			content, err := t.convertUserContent(msg)
			if err != nil {
				return "", nil, err
			}
			claudeMsgs = append(claudeMsgs, provider.ClaudeMessage{
				Role:    "user",
				Content: content,
			})

		case "assistant":
			content, err := t.convertAssistantContent(msg)
			if err != nil {
				return "", nil, err
			}
			claudeMsgs = append(claudeMsgs, provider.ClaudeMessage{
				Role:    "assistant",
				Content: content,
			})

		case "tool":
			// Tool results are added to the previous user message or create a new one
			toolResult := t.convertToolResult(msg)
			claudeMsgs = append(claudeMsgs, provider.ClaudeMessage{
				Role:    "user",
				Content: []provider.ClaudeContentBlock{toolResult},
			})
		}
	}

	return systemPrompt, claudeMsgs, nil
}

// convertUserContent converts user message content to Claude format
func (t *ClaudeTransformer) convertUserContent(msg OpenAIMessage) (interface{}, error) {
	parts, err := msg.GetContentParts()
	if err != nil {
		return nil, err
	}

	// Simple text content
	if len(parts) == 1 && parts[0].Type == "text" {
		return parts[0].Text, nil
	}

	// Multimodal content
	var blocks []provider.ClaudeContentBlock
	for _, part := range parts {
		switch part.Type {
		case "text":
			blocks = append(blocks, provider.ClaudeContentBlock{
				Type: "text",
				Text: part.Text,
			})
		case "image_url":
			if part.ImageURL != nil {
				block, err := t.convertImageURL(part.ImageURL)
				if err != nil {
					return nil, err
				}
				blocks = append(blocks, block)
			}
		}
	}

	return blocks, nil
}

// convertImageURL converts an OpenAI image URL to Claude format
func (t *ClaudeTransformer) convertImageURL(img *ImageURL) (provider.ClaudeContentBlock, error) {
	block := provider.ClaudeContentBlock{Type: "image"}

	// Check if it's a data URL (base64)
	if strings.HasPrefix(img.URL, "data:") {
		parts := strings.SplitN(img.URL, ",", 2)
		if len(parts) != 2 {
			return block, fmt.Errorf("invalid data URL format")
		}

		// Extract media type from "data:image/png;base64"
		header := strings.TrimPrefix(parts[0], "data:")
		mediaType := strings.TrimSuffix(header, ";base64")

		block.Source = &provider.ClaudeSource{
			Type:      "base64",
			MediaType: mediaType,
			Data:      parts[1],
		}
	} else {
		// Regular URL - Claude doesn't support URL directly, need to fetch and encode
		// For now, we'll pass it as URL and let the caller handle it
		block.Source = &provider.ClaudeSource{
			Type: "url",
			URL:  img.URL,
		}
	}

	return block, nil
}

// convertAssistantContent converts assistant message content to Claude format
func (t *ClaudeTransformer) convertAssistantContent(msg OpenAIMessage) (interface{}, error) {
	var blocks []provider.ClaudeContentBlock

	// Add text content if present
	if s, ok := msg.Content.(string); ok && s != "" {
		blocks = append(blocks, provider.ClaudeContentBlock{
			Type: "text",
			Text: s,
		})
	}

	// Add tool calls if present
	for _, tc := range msg.ToolCalls {
		blocks = append(blocks, provider.ClaudeContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: json.RawMessage(tc.Function.Arguments),
		})
	}

	// Return simple string if only text
	if len(blocks) == 1 && blocks[0].Type == "text" {
		return blocks[0].Text, nil
	}

	return blocks, nil
}

// convertToolResult converts a tool result message to Claude format
func (t *ClaudeTransformer) convertToolResult(msg OpenAIMessage) provider.ClaudeContentBlock {
	content := msg.GetStringContent()
	return provider.ClaudeContentBlock{
		Type:      "tool_result",
		ToolUseID: msg.ToolCallID,
		Content:   content,
	}
}

// convertTools converts OpenAI tools to Claude format
func (t *ClaudeTransformer) convertTools(tools []OpenAITool) []provider.ClaudeTool {
	claudeTools := make([]provider.ClaudeTool, 0, len(tools))
	for _, tool := range tools {
		if tool.Type == "function" {
			claudeTools = append(claudeTools, provider.ClaudeTool{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				InputSchema: tool.Function.Parameters,
			})
		}
	}
	return claudeTools
}

// convertToolChoice converts OpenAI tool_choice to Claude format
func (t *ClaudeTransformer) convertToolChoice(choice interface{}) interface{} {
	if choice == nil {
		return nil
	}

	switch v := choice.(type) {
	case string:
		switch v {
		case "auto":
			return map[string]string{"type": "auto"}
		case "none":
			return map[string]string{"type": "none"}
		case "required":
			return map[string]string{"type": "any"}
		}
	case map[string]interface{}:
		if v["type"] == "function" {
			if fn, ok := v["function"].(map[string]interface{}); ok {
				if name, ok := fn["name"].(string); ok {
					return map[string]interface{}{
						"type": "tool",
						"name": name,
					}
				}
			}
		}
	}

	return nil
}

// convertStopSequences converts stop sequences
func (t *ClaudeTransformer) convertStopSequences(stop interface{}) []string {
	switch v := stop.(type) {
	case string:
		return []string{v}
	case []interface{}:
		seqs := make([]string, 0, len(v))
		for _, s := range v {
			if str, ok := s.(string); ok {
				seqs = append(seqs, str)
			}
		}
		return seqs
	case []string:
		return v
	}
	return nil
}

// TransformResponse transforms Claude response to OpenAI format
func (t *ClaudeTransformer) TransformResponse(resp interface{}) (*OpenAIResponse, error) {
	claudeResp, ok := resp.(*provider.ClaudeResponse)
	if !ok {
		return nil, ErrInvalidResponse
	}

	openAIResp := &OpenAIResponse{
		ID:      claudeResp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   claudeResp.Model,
		Usage: &OpenAIUsage{
			PromptTokens:     claudeResp.Usage.InputTokens,
			CompletionTokens: claudeResp.Usage.OutputTokens,
			TotalTokens:      claudeResp.Usage.InputTokens + claudeResp.Usage.OutputTokens,
		},
	}

	// Convert content to OpenAI message format
	msg := &OpenAIMessage{
		Role: "assistant",
	}

	var textContent strings.Builder
	var toolCalls []ToolCall

	for _, block := range claudeResp.Content {
		switch block.Type {
		case "text":
			textContent.WriteString(block.Text)
		case "tool_use":
			toolCalls = append(toolCalls, ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: FunctionCall{
					Name:      block.Name,
					Arguments: string(block.Input),
				},
			})
		}
	}

	if textContent.Len() > 0 {
		msg.Content = textContent.String()
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}

	// Map stop reason
	finishReason := t.mapStopReason(claudeResp.StopReason)

	openAIResp.Choices = []Choice{
		{
			Index:        0,
			Message:      msg,
			FinishReason: finishReason,
		},
	}

	return openAIResp, nil
}

// mapStopReason maps Claude stop reason to OpenAI format
func (t *ClaudeTransformer) mapStopReason(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	case "tool_use":
		return "tool_calls"
	default:
		return reason
	}
}

// TransformStreamEvent transforms Claude stream event to OpenAI format
func (t *ClaudeTransformer) TransformStreamEvent(event interface{}) (*OpenAIStreamChunk, error) {
	// Get stable stream context for consistent id/model across all chunks
	streamID, streamModel, streamCreated := t.getStreamContext()

	chunk := &OpenAIStreamChunk{
		Object:  "chat.completion.chunk",
		Created: streamCreated,
	}
	// Use stream context if set, otherwise fall back to per-chunk values
	if streamCreated == 0 {
		chunk.Created = time.Now().Unix()
	}

	switch e := event.(type) {
	case *provider.ClaudeMessageStart:
		// Use stream context id/model if set, otherwise use from event
		if streamID != "" {
			chunk.ID = streamID
		} else {
			chunk.ID = e.Message.ID
		}
		if streamModel != "" {
			chunk.Model = streamModel
		} else {
			chunk.Model = e.Message.Model
		}
		chunk.Choices = []Choice{
			{
				Index: 0,
				Delta: &OpenAIMessage{Role: "assistant"},
			},
		}

	case *provider.ClaudeContentBlockStart:
		// Apply stream context for consistent id/model
		chunk.ID = streamID
		chunk.Model = streamModel
		if e.ContentBlock.Type == "tool_use" {
			// Tool call start - include index so clients can track multiple parallel tool calls
			toolCallIndex := e.Index
			chunk.Choices = []Choice{
				{
					Index: 0, // Choice index is always 0 for Claude (single candidate)
					Delta: &OpenAIMessage{
						ToolCalls: []ToolCall{
							{
								Index: &toolCallIndex,
								ID:    e.ContentBlock.ID,
								Type:  "function",
								Function: FunctionCall{
									Name:      e.ContentBlock.Name,
									Arguments: "",
								},
							},
						},
					},
				},
			}
		} else {
			// Text block start - empty delta
			chunk.Choices = []Choice{
				{Index: e.Index, Delta: &OpenAIMessage{}},
			}
		}

	case *provider.ClaudeContentBlockDelta:
		// Apply stream context for consistent id/model
		chunk.ID = streamID
		chunk.Model = streamModel
		if e.Delta.Type == "text_delta" {
			chunk.Choices = []Choice{
				{
					Index: e.Index,
					Delta: &OpenAIMessage{Content: e.Delta.Text},
				},
			}
		} else if e.Delta.Type == "input_json_delta" {
			// Tool call argument delta - include index to identify which tool call this delta belongs to
			// OpenAI streaming requires the index field on every tool_call delta so clients can
			// associate argument increments with the correct tool call started in a previous chunk
			toolCallIndex := e.Index
			chunk.Choices = []Choice{
				{
					Index: 0, // Choice index is always 0 for Claude (single candidate)
					Delta: &OpenAIMessage{
						ToolCalls: []ToolCall{
							{
								Index: &toolCallIndex,
								Function: FunctionCall{
									Arguments: e.Delta.PartialJSON,
								},
							},
						},
					},
				},
			}
		}

	case *provider.ClaudeMessageDelta:
		// Apply stream context for consistent id/model
		chunk.ID = streamID
		chunk.Model = streamModel
		finishReason := t.mapStopReason(e.Delta.StopReason)
		chunk.Choices = []Choice{
			{
				Index:        0,
				Delta:        &OpenAIMessage{},
				FinishReason: finishReason,
			},
		}
		if e.Usage.OutputTokens > 0 {
			chunk.Usage = &OpenAIUsage{
				CompletionTokens: e.Usage.OutputTokens,
			}
		}

	default:
		return nil, nil // Skip unknown events
	}

	return chunk, nil
}

// Ensure ClaudeTransformer implements Transformer and StreamContextSetter interfaces
var (
	_ Transformer          = (*ClaudeTransformer)(nil)
	_ StreamContextSetter  = (*ClaudeTransformer)(nil)
)

