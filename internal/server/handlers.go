package server

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/unifyai-proxy/unifyai-proxy/internal/account"
	"github.com/unifyai-proxy/unifyai-proxy/internal/provider"
	"github.com/unifyai-proxy/unifyai-proxy/internal/sse"
	"github.com/unifyai-proxy/unifyai-proxy/internal/transformer"
)

// DefaultMaxRequestSize is the default maximum request body size (10MB)
const DefaultMaxRequestSize = 10 * 1024 * 1024

// DefaultMaxEventSize is the default maximum SSE event size (1MB)
const DefaultMaxEventSize = 1024 * 1024

// Handlers contains HTTP handlers for the API
type Handlers struct {
	providers       map[string]provider.Provider
	transformers    map[string]transformer.Transformer
	accountSelector account.AccountSelector
	maxRequestSize  int64
	maxEventSize    int
	mu              sync.RWMutex // Protects providers and transformers maps
}

// HandlersConfig contains configuration for handlers
type HandlersConfig struct {
	MaxRequestSize  int64
	MaxEventSize    int
	AccountSelector account.AccountSelector
}

// NewHandlers creates new handlers
func NewHandlers() *Handlers {
	return &Handlers{
		providers:      make(map[string]provider.Provider),
		transformers:   make(map[string]transformer.Transformer),
		maxRequestSize: DefaultMaxRequestSize,
		maxEventSize:   DefaultMaxEventSize,
	}
}

// NewHandlersWithConfig creates new handlers with configuration
func NewHandlersWithConfig(cfg HandlersConfig) *Handlers {
	maxSize := cfg.MaxRequestSize
	if maxSize <= 0 {
		maxSize = DefaultMaxRequestSize
	}
	maxEventSize := cfg.MaxEventSize
	if maxEventSize <= 0 {
		maxEventSize = DefaultMaxEventSize
	}
	return &Handlers{
		providers:       make(map[string]provider.Provider),
		transformers:    make(map[string]transformer.Transformer),
		accountSelector: cfg.AccountSelector,
		maxRequestSize:  maxSize,
		maxEventSize:    maxEventSize,
	}
}

// SetAccountSelector sets the account selector (for late binding)
func (h *Handlers) SetAccountSelector(selector account.AccountSelector) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.accountSelector = selector
}

// RegisterProvider registers a provider (thread-safe)
func (h *Handlers) RegisterProvider(name string, p provider.Provider) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.providers[name] = p
}

// RegisterTransformer registers a transformer (thread-safe)
func (h *Handlers) RegisterTransformer(name string, t transformer.Transformer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.transformers[name] = t
}

// HealthHandler handles /health endpoint
func (h *Handlers) HealthHandler(w http.ResponseWriter, r *http.Request) {
	resp := HealthResponse{
		Status:    "ok",
		Timestamp: time.Now().Unix(),
	}
	WriteJSON(w, http.StatusOK, resp)
}

// ReadyHandler handles /ready endpoint
func (h *Handlers) ReadyHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: Check actual readiness (provider connections, etc.)
	resp := HealthResponse{
		Status:    "ready",
		Timestamp: time.Now().Unix(),
	}
	WriteJSON(w, http.StatusOK, resp)
}

// ModelsHandler handles /v1/models endpoint
func (h *Handlers) ModelsHandler(w http.ResponseWriter, r *http.Request) {
	var models []ModelData

	h.mu.RLock()
	for name, p := range h.providers {
		for _, modelName := range p.SupportedModels() {
			models = append(models, ModelData{
				ID:      modelName,
				Object:  "model",
				Created: time.Now().Unix(),
				OwnedBy: name,
				Root:    modelName,
			})
		}
	}
	h.mu.RUnlock()

	resp := ModelResponse{
		Object: "list",
		Data:   models,
	}
	WriteJSON(w, http.StatusOK, resp)
}

// ChatCompletionsHandler handles /v1/chat/completions endpoint
func (h *Handlers) ChatCompletionsHandler(w http.ResponseWriter, r *http.Request) {
	// Limit request body size to prevent memory exhaustion attacks
	r.Body = http.MaxBytesReader(w, r.Body, h.maxRequestSize)
	defer r.Body.Close()

	// Parse request
	body, err := io.ReadAll(r.Body)
	if err != nil {
		// Check if error is due to request body being too large
		if err.Error() == "http: request body too large" {
			WriteError(w, http.StatusRequestEntityTooLarge, "request_too_large", "Request body exceeds maximum allowed size")
			return
		}
		WriteError(w, http.StatusBadRequest, "invalid_request", "Failed to read request body")
		return
	}

	var req transformer.OpenAIRequest
	if err := json.Unmarshal(body, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON in request body")
		return
	}

	// Validate request
	if req.Model == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "Model is required")
		return
	}

	// Find provider for model (thread-safe)
	var selectedProvider provider.Provider
	var selectedTransformer transformer.Transformer

	h.mu.RLock()
	for name, p := range h.providers {
		if p.SupportsModel(req.Model) {
			selectedProvider = p
			selectedTransformer = h.transformers[name]
			break
		}
	}
	h.mu.RUnlock()

	if selectedProvider == nil {
		WriteError(w, http.StatusBadRequest, "model_not_found", "Model not found: "+req.Model)
		return
	}

	if selectedTransformer == nil {
		WriteError(w, http.StatusInternalServerError, "configuration_error", "No transformer configured for provider")
		return
	}

	// Transform request
	providerReq, err := selectedTransformer.TransformRequest(&req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "transform_error", err.Error())
		return
	}

	ctx := r.Context()

	// Handle streaming
	if req.Stream {
		// Set stream context if transformer supports it (for stable id/model across chunks)
		if setter, ok := selectedTransformer.(transformer.StreamContextSetter); ok {
			setter.SetStreamContext(req.Model)
			defer setter.ClearStreamContext()
		}

		eventCh, err := selectedProvider.StreamRequest(ctx, providerReq)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "provider_error", err.Error())
			return
		}

		config := sse.DefaultStreamConfig()
		config.MaxEventSize = h.maxEventSize
		if err := sse.ProxyStream(ctx, w, eventCh, selectedTransformer, config); err != nil {
			// Error already written to stream
			return
		}
		return
	}

	// Handle non-streaming
	providerResp, err := selectedProvider.SendRequest(ctx, providerReq)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "provider_error", err.Error())
		return
	}

	openAIResp, err := selectedTransformer.TransformResponse(providerResp)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "transform_error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, openAIResp)
}

// RegisterRoutes registers all routes
func (h *Handlers) RegisterRoutes(router *Router) {
	router.GET("/health", h.HealthHandler)
	router.GET("/ready", h.ReadyHandler)
	router.GET("/v1/models", h.ModelsHandler)
	router.POST("/v1/chat/completions", h.ChatCompletionsHandler)
}

