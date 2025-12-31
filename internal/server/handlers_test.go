package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewHandlers(t *testing.T) {
	h := NewHandlers()

	if h == nil {
		t.Fatal("expected handlers, got nil")
	}

	if h.maxRequestSize != DefaultMaxRequestSize {
		t.Errorf("expected maxRequestSize %d, got %d", DefaultMaxRequestSize, h.maxRequestSize)
	}

	if h.maxEventSize != DefaultMaxEventSize {
		t.Errorf("expected maxEventSize %d, got %d", DefaultMaxEventSize, h.maxEventSize)
	}
}

func TestNewHandlersWithConfig(t *testing.T) {
	cfg := HandlersConfig{
		MaxRequestSize: 5 * 1024 * 1024,
		MaxEventSize:   512 * 1024,
	}

	h := NewHandlersWithConfig(cfg)

	if h.maxRequestSize != cfg.MaxRequestSize {
		t.Errorf("expected maxRequestSize %d, got %d", cfg.MaxRequestSize, h.maxRequestSize)
	}

	if h.maxEventSize != cfg.MaxEventSize {
		t.Errorf("expected maxEventSize %d, got %d", cfg.MaxEventSize, h.maxEventSize)
	}
}

func TestNewHandlersWithConfig_Defaults(t *testing.T) {
	// Test that zero values get defaults
	cfg := HandlersConfig{}
	h := NewHandlersWithConfig(cfg)

	if h.maxRequestSize != DefaultMaxRequestSize {
		t.Errorf("expected default maxRequestSize, got %d", h.maxRequestSize)
	}

	if h.maxEventSize != DefaultMaxEventSize {
		t.Errorf("expected default maxEventSize, got %d", h.maxEventSize)
	}
}

func TestHealthHandler(t *testing.T) {
	h := NewHandlers()

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	h.HealthHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var resp HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "ok" {
		t.Errorf("expected status 'ok', got %q", resp.Status)
	}

	if resp.Timestamp == 0 {
		t.Error("expected non-zero timestamp")
	}
}

func TestReadyHandler(t *testing.T) {
	h := NewHandlers()

	req := httptest.NewRequest("GET", "/ready", nil)
	rec := httptest.NewRecorder()

	h.ReadyHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var resp HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "ready" {
		t.Errorf("expected status 'ready', got %q", resp.Status)
	}
}

func TestModelsHandler_Empty(t *testing.T) {
	h := NewHandlers()

	req := httptest.NewRequest("GET", "/v1/models", nil)
	rec := httptest.NewRecorder()

	h.ModelsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var resp ModelResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Object != "list" {
		t.Errorf("expected object 'list', got %q", resp.Object)
	}
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()

	data := map[string]string{"key": "value"}
	WriteJSON(rec, http.StatusOK, data)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", ct)
	}
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()

	WriteError(rec, http.StatusBadRequest, "bad_request", "Test error message")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Error.Code != "bad_request" {
		t.Errorf("expected code 'bad_request', got %q", resp.Error.Code)
	}
}

