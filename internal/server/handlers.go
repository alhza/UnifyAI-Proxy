package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/unifyai-proxy/unifyai-proxy/internal/account"
	"github.com/unifyai-proxy/unifyai-proxy/internal/metrics"
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
	providers        map[string]provider.Provider
	providerNames    []string // Ordered list of provider names for deterministic selection
	transformers     map[string]transformer.Transformer
	accountSelector  account.AccountSelector
	metrics          *metrics.Metrics
	maxRequestSize   int64
	maxEventSize     int
	accountReloader  func() // Function to reload accounts from token store
	mu               sync.RWMutex // Protects providers and transformers maps
}

// HandlersConfig contains configuration for handlers
type HandlersConfig struct {
	MaxRequestSize  int64
	MaxEventSize    int
	AccountSelector account.AccountSelector
	Metrics         *metrics.Metrics
}

// NewHandlers creates new handlers
func NewHandlers() *Handlers {
	return &Handlers{
		providers:      make(map[string]provider.Provider),
		providerNames:  make([]string, 0),
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
	m := cfg.Metrics
	if m == nil {
		m = metrics.New() // Create default metrics if not provided
	}
	return &Handlers{
		providers:       make(map[string]provider.Provider),
		providerNames:   make([]string, 0),
		transformers:    make(map[string]transformer.Transformer),
		accountSelector: cfg.AccountSelector,
		metrics:         m,
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
// Providers are selected in alphabetical order by name for deterministic behavior
func (h *Handlers) RegisterProvider(name string, p provider.Provider) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Only add to names list if this is a new provider
	if _, exists := h.providers[name]; !exists {
		h.providerNames = append(h.providerNames, name)
		// Keep names sorted for deterministic selection order
		sort.Strings(h.providerNames)
	}
	h.providers[name] = p
}

// RegisterTransformer registers a transformer (thread-safe)
func (h *Handlers) RegisterTransformer(name string, t transformer.Transformer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.transformers[name] = t
}

// SetAccountReloader sets the function to reload accounts from token store
// This function will be called when the /admin/reload-accounts endpoint is hit
func (h *Handlers) SetAccountReloader(reloader func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.accountReloader = reloader
}

// ReloadAccountsHandler handles POST /admin/reload-accounts endpoint
// This triggers a reload of accounts from the token store
func (h *Handlers) ReloadAccountsHandler(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	reloader := h.accountReloader
	h.mu.RUnlock()

	if reloader == nil {
		WriteError(w, http.StatusServiceUnavailable, "not_configured", "Account reloader not configured")
		return
	}

	reloader()

	slog.Info("Accounts reloaded via admin API")
	WriteJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "Accounts reloaded successfully",
	})
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
	// Iterate in sorted order for deterministic response
	for _, name := range h.providerNames {
		p := h.providers[name]
		if p == nil {
			continue
		}
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
	startTime := time.Now()

	// Limit request body size to prevent memory exhaustion attacks
	r.Body = http.MaxBytesReader(w, r.Body, h.maxRequestSize)
	defer r.Body.Close()

	// Parse request
	body, err := io.ReadAll(r.Body)
	if err != nil {
		// Check if error is due to request body being too large using proper type check
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
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

	// Find provider for model with failover support
	// Try each provider that supports the model until we find one with an available account
	var selectedProvider provider.Provider
	var selectedTransformer transformer.Transformer
	var selectedAccount *account.Account
	var providerName string
	var lastPickErr error

	h.mu.RLock()
	// Collect all providers that support this model
	var candidateProviders []string
	for _, name := range h.providerNames {
		p := h.providers[name]
		if p != nil && p.SupportsModel(req.Model) {
			candidateProviders = append(candidateProviders, name)
		}
	}
	h.mu.RUnlock()

	if len(candidateProviders) == 0 {
		WriteError(w, http.StatusBadRequest, "model_not_found", "Model not found: "+req.Model)
		return
	}

	// Try each provider with failover
	for _, name := range candidateProviders {
		h.mu.RLock()
		p := h.providers[name]
		t := h.transformers[name]
		h.mu.RUnlock()

		if p == nil || t == nil {
			continue
		}

		// Try to pick an account for this provider
		if h.accountSelector != nil {
			acc, err := h.accountSelector.Pick(req.Model, name)
			if err != nil {
				lastPickErr = err
				slog.Debug("Provider unavailable, trying next",
					"provider", name,
					"model", req.Model,
					"error", err)
				continue // Try next provider
			}
			selectedAccount = acc
		}

		// Found a working provider
		selectedProvider = p
		selectedTransformer = t
		providerName = name
		break
	}

	if selectedProvider == nil {
		// All providers failed
		errMsg := "All providers unavailable for model: " + req.Model
		if lastPickErr != nil {
			errMsg = lastPickErr.Error()
		}
		WriteError(w, http.StatusServiceUnavailable, "no_available_provider", errMsg)
		return
	}

	// Ensure account is released after request
	if selectedAccount != nil {
		defer selectedAccount.Release()
	}

	// Transform request
	providerReq, err := selectedTransformer.TransformRequest(&req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "transform_error", err.Error())
		return
	}

	ctx := r.Context()

	// If an account was selected, pass its TokenID to the provider via context
	// This ensures the provider uses the correct token for this specific account
	if selectedAccount != nil {
		ctx = provider.WithTokenID(ctx, selectedAccount.TokenID)
	}

	// Track active requests for metrics
	if h.metrics != nil {
		h.metrics.IncActiveRequests(providerName)
		defer h.metrics.DecActiveRequests(providerName)
	}

	// Handle streaming
	if req.Stream {
		// Create stream context if transformer supports it (for stable id/model across chunks)
		var streamCtx *transformer.StreamContext
		if setter, ok := selectedTransformer.(transformer.StreamContextSetter); ok {
			streamCtx = setter.NewStreamContext(req.Model)
		}

		eventCh, err := selectedProvider.StreamRequest(ctx, providerReq)
		if err != nil {
			h.markAccountResult(selectedAccount, false, err, time.Since(startTime), 0, providerName, req.Model)
			WriteError(w, http.StatusInternalServerError, "provider_error", err.Error())
			return
		}

		config := sse.DefaultStreamConfig()
		config.MaxEventSize = h.maxEventSize
		if err := sse.ProxyStreamWithContext(ctx, w, eventCh, selectedTransformer, streamCtx, config); err != nil {
			h.markAccountResult(selectedAccount, false, err, time.Since(startTime), 0, providerName, req.Model)
			// Error already written to stream
			return
		}

		// Mark success for streaming (token count not available for streams)
		h.markAccountResult(selectedAccount, true, nil, time.Since(startTime), 0, providerName, req.Model)
		return
	}

	// Handle non-streaming
	providerResp, err := selectedProvider.SendRequest(ctx, providerReq)
	if err != nil {
		h.markAccountResult(selectedAccount, false, err, time.Since(startTime), 0, providerName, req.Model)
		WriteError(w, http.StatusInternalServerError, "provider_error", err.Error())
		return
	}

	openAIResp, err := selectedTransformer.TransformResponse(providerResp)
	if err != nil {
		h.markAccountResult(selectedAccount, false, err, time.Since(startTime), 0, providerName, req.Model)
		WriteError(w, http.StatusInternalServerError, "transform_error", err.Error())
		return
	}

	// Extract token usage if available
	var tokensUsed int64
	if openAIResp != nil && openAIResp.Usage != nil {
		tokensUsed = int64(openAIResp.Usage.TotalTokens)
	}

	h.markAccountResult(selectedAccount, true, nil, time.Since(startTime), tokensUsed, providerName, req.Model)
	WriteJSON(w, http.StatusOK, openAIResp)
}

// markAccountResult updates account state based on request result and records metrics
func (h *Handlers) markAccountResult(acc *account.Account, success bool, err error, responseTime time.Duration, tokensUsed int64, providerName, model string) {
	// Record metrics
	if h.metrics != nil {
		status := "success"
		if !success {
			status = "error"
		}
		h.metrics.RecordRequest(providerName, model, status, responseTime.Seconds())
		if tokensUsed > 0 {
			// Estimate prompt/completion split (rough 30/70 split for typical usage)
			promptTokens := int(tokensUsed * 30 / 100)
			completionTokens := int(tokensUsed) - promptTokens
			h.metrics.RecordTokens(providerName, promptTokens, completionTokens)
		}
	}

	// Update account state
	if acc == nil || h.accountSelector == nil {
		return
	}

	result := account.Result{
		Success:      success,
		TokensUsed:   tokensUsed,
		ResponseTime: responseTime,
		Error:        err,
	}

	if !success && err != nil {
		result.ErrorType, result.ErrorCode = classifyError(err)
		result.ShouldSwitch = result.ErrorType == account.ErrorTypeQuotaExceeded ||
			result.ErrorType == account.ErrorTypeRateLimited ||
			result.ErrorType == account.ErrorTypeAuthError
		result.ShouldRetry = result.ErrorType == account.ErrorTypeNetworkError ||
			result.ErrorType == account.ErrorTypeServerError
	}

	h.accountSelector.MarkResult(acc, result)
}

// classifyError classifies an error into an ErrorType and HTTP status code
func classifyError(err error) (account.ErrorType, int) {
	if err == nil {
		return account.ErrorTypeNone, 0
	}

	// Check for known provider errors
	if errors.Is(err, provider.ErrQuotaExceeded) {
		return account.ErrorTypeQuotaExceeded, 429
	}
	if errors.Is(err, provider.ErrRateLimited) {
		return account.ErrorTypeRateLimited, 429
	}
	if errors.Is(err, provider.ErrAuthenticationFailed) || errors.Is(err, provider.ErrTokenExpired) {
		return account.ErrorTypeAuthError, 401
	}
	if errors.Is(err, provider.ErrServerError) {
		return account.ErrorTypeServerError, 500
	}
	if errors.Is(err, provider.ErrNetworkError) {
		return account.ErrorTypeNetworkError, 0
	}

	// Check for provider error type
	var providerErr *provider.ProviderError
	if errors.As(err, &providerErr) {
		switch providerErr.StatusCode {
		case 401, 403:
			return account.ErrorTypeAuthError, providerErr.StatusCode
		case 429:
			// Distinguish between rate limit and quota exceeded based on error message
			if providerErr.Type == "quota_exceeded" {
				return account.ErrorTypeQuotaExceeded, 429
			}
			return account.ErrorTypeRateLimited, 429
		case 500, 502, 503, 504:
			return account.ErrorTypeServerError, providerErr.StatusCode
		}
	}

	return account.ErrorTypeNetworkError, 0
}

// StatsHandler handles /stats endpoint for metrics
func (h *Handlers) StatsHandler(w http.ResponseWriter, r *http.Request) {
	if h.metrics == nil {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"message": "metrics not enabled",
		})
		return
	}

	stats := h.metrics.GetStats()

	// Build response with provider info
	response := StatsResponse{
		Timestamp: time.Now().Unix(),
		Providers: make(map[string]ProviderStatsResponse),
	}

	for providerName, ps := range stats.Providers {
		response.Providers[providerName] = ProviderStatsResponse{
			TotalRequests:  ps.TotalRequests,
			ActiveRequests: ps.ActiveRequests,
			Latency: LatencyStats{
				Count:      ps.Latency.Count,
				AvgMs:      float64(ps.Latency.Average.Milliseconds()),
				TotalMs:    float64(ps.Latency.Sum.Milliseconds()),
			},
			Tokens: TokenStats{
				Prompt:     ps.Tokens.Prompt,
				Completion: ps.Tokens.Completion,
				Total:      ps.Tokens.Total,
			},
			Errors: ps.Errors,
		}
	}

	WriteJSON(w, http.StatusOK, response)
}

// StatsResponse is the response format for /stats endpoint
type StatsResponse struct {
	Timestamp int64                            `json:"timestamp"`
	Providers map[string]ProviderStatsResponse `json:"providers"`
}

// ProviderStatsResponse contains per-provider statistics
type ProviderStatsResponse struct {
	TotalRequests  int64            `json:"total_requests"`
	ActiveRequests int64            `json:"active_requests"`
	Latency        LatencyStats     `json:"latency"`
	Tokens         TokenStats       `json:"tokens"`
	Errors         map[string]int64 `json:"errors,omitempty"`
}

// LatencyStats contains latency statistics
type LatencyStats struct {
	Count   int64   `json:"count"`
	AvgMs   float64 `json:"avg_ms"`
	TotalMs float64 `json:"total_ms"`
}

// TokenStats contains token usage statistics
type TokenStats struct {
	Prompt     int64 `json:"prompt"`
	Completion int64 `json:"completion"`
	Total      int64 `json:"total"`
}

// RegisterRoutes registers all routes
func (h *Handlers) RegisterRoutes(router *Router) {
	router.GET("/health", h.HealthHandler)
	router.GET("/ready", h.ReadyHandler)
	router.GET("/stats", h.StatsHandler)
	router.GET("/v1/models", h.ModelsHandler)
	router.POST("/v1/chat/completions", h.ChatCompletionsHandler)
	router.POST("/admin/reload-accounts", h.ReloadAccountsHandler)
}

