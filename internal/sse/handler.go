package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/unifyai-proxy/unifyai-proxy/internal/provider"
	"github.com/unifyai-proxy/unifyai-proxy/internal/transformer"
)

// StreamHandler handles streaming responses from providers
type StreamHandler struct {
	transformer  transformer.Transformer
	keepalive    time.Duration
	maxEventSize int
}

// StreamHandlerConfig contains configuration for StreamHandler
type StreamHandlerConfig struct {
	Transformer  transformer.Transformer
	Keepalive    time.Duration
	MaxEventSize int
}

// NewStreamHandler creates a new stream handler
func NewStreamHandler(t transformer.Transformer, keepalive time.Duration) *StreamHandler {
	return &StreamHandler{
		transformer:  t,
		keepalive:    keepalive,
		maxEventSize: 0, // Use default
	}
}

// NewStreamHandlerWithConfig creates a new stream handler with full configuration
func NewStreamHandlerWithConfig(cfg StreamHandlerConfig) *StreamHandler {
	return &StreamHandler{
		transformer:  cfg.Transformer,
		keepalive:    cfg.Keepalive,
		maxEventSize: cfg.MaxEventSize,
	}
}

// HandleStream handles a streaming response from a provider
func (h *StreamHandler) HandleStream(
	ctx context.Context,
	w http.ResponseWriter,
	eventCh <-chan provider.StreamEvent,
) error {
	// Create SSE writer with configuration
	opts := []WriterOption{}
	if h.keepalive > 0 {
		opts = append(opts, WithKeepalive(h.keepalive))
	}
	if h.maxEventSize > 0 {
		opts = append(opts, WithMaxEventSize(h.maxEventSize))
	}

	sseWriter, err := NewWriter(w, opts...)
	if err != nil {
		return fmt.Errorf("failed to create SSE writer: %w", err)
	}
	defer sseWriter.Close()

	// Process events
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case event, ok := <-eventCh:
			if !ok {
				// Channel closed, send done
				return sseWriter.WriteDone()
			}

			if err := h.handleEvent(sseWriter, &event); err != nil {
				return err
			}
		}
	}
}

// handleEvent processes a single stream event
func (h *StreamHandler) handleEvent(w *Writer, event *provider.StreamEvent) error {
	switch event.Type {
	case provider.StreamEventTypeError:
		if event.Error != nil {
			return w.WriteError(event.Error)
		}
		return nil

	case provider.StreamEventTypeDone:
		return w.WriteDone()

	case provider.StreamEventTypeData, provider.StreamEventTypeToolCall, provider.StreamEventTypeThinking:
		// Transform to OpenAI format
		if h.transformer != nil && event.Data != nil {
			chunk, err := h.transformer.TransformStreamEvent(event.Data)
			if err != nil {
				return fmt.Errorf("transform error: %w", err)
			}
			if chunk != nil {
				return w.WriteJSON(chunk)
			}
		} else if event.RawData != nil {
			// Write raw data if no transformer
			return w.WriteEvent(&Event{RawData: event.RawData})
		}
		return nil

	default:
		// Unknown event type, skip
		return nil
	}
}

// StreamConfig contains configuration for stream handling
type StreamConfig struct {
	Keepalive       time.Duration
	BufferSize      int
	MaxEventSize    int
	IncludeUsage    bool
	TransformEvents bool
}

// DefaultStreamConfig returns the default stream configuration
func DefaultStreamConfig() StreamConfig {
	return StreamConfig{
		Keepalive:       30 * time.Second,
		BufferSize:      100,
		MaxEventSize:    1024 * 1024, // 1MB
		IncludeUsage:    true,
		TransformEvents: true,
	}
}

// ProxyStream proxies a streaming response from an upstream provider
func ProxyStream(
	ctx context.Context,
	w http.ResponseWriter,
	eventCh <-chan provider.StreamEvent,
	t transformer.Transformer,
	config StreamConfig,
) error {
	handler := NewStreamHandlerWithConfig(StreamHandlerConfig{
		Transformer:  t,
		Keepalive:    config.Keepalive,
		MaxEventSize: config.MaxEventSize,
	})
	return handler.HandleStream(ctx, w, eventCh)
}

// WriteOpenAIChunk writes an OpenAI-compatible stream chunk
func WriteOpenAIChunk(w *Writer, chunk *transformer.OpenAIStreamChunk) error {
	data, err := json.Marshal(chunk)
	if err != nil {
		return err
	}
	return w.WriteEvent(&Event{RawData: data})
}

// CreateOpenAIChunk creates a simple OpenAI stream chunk with text content
func CreateOpenAIChunk(id, model, content string, finishReason string) *transformer.OpenAIStreamChunk {
	chunk := &transformer.OpenAIStreamChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []transformer.Choice{
			{
				Index: 0,
				Delta: &transformer.OpenAIMessage{},
			},
		},
	}

	if content != "" {
		chunk.Choices[0].Delta.Content = content
	}
	if finishReason != "" {
		chunk.Choices[0].FinishReason = finishReason
	}

	return chunk
}

