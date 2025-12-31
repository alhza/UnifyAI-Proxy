package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// Metrics provides request metrics collection and aggregation
type Metrics struct {
	// Request counters by provider
	requestCounters map[string]*atomic.Int64
	// Error counters by provider and error type
	errorCounters map[string]map[string]*atomic.Int64
	// Latency histograms by provider
	latencyHistograms map[string]*LatencyHistogram
	// Token usage by provider
	tokenCounters map[string]*TokenCounter
	// Active requests gauge
	activeRequests map[string]*atomic.Int64
	mu             sync.RWMutex
}

// TokenCounter tracks token usage
type TokenCounter struct {
	PromptTokens     atomic.Int64
	CompletionTokens atomic.Int64
	TotalTokens      atomic.Int64
}

// LatencyHistogram tracks request latency distribution
type LatencyHistogram struct {
	buckets []int64          // counts per bucket
	bounds  []time.Duration  // bucket boundaries
	sum     atomic.Int64     // sum of all durations in nanoseconds
	count   atomic.Int64     // total count
	mu      sync.Mutex
}

// NewLatencyHistogram creates a new latency histogram with default buckets
func NewLatencyHistogram() *LatencyHistogram {
	return &LatencyHistogram{
		buckets: make([]int64, 10),
		bounds: []time.Duration{
			10 * time.Millisecond,
			50 * time.Millisecond,
			100 * time.Millisecond,
			250 * time.Millisecond,
			500 * time.Millisecond,
			1 * time.Second,
			2 * time.Second,
			5 * time.Second,
			10 * time.Second,
		},
	}
}

// Observe records a latency observation
func (h *LatencyHistogram) Observe(d time.Duration) {
	h.sum.Add(int64(d))
	h.count.Add(1)

	h.mu.Lock()
	defer h.mu.Unlock()

	for i, bound := range h.bounds {
		if d <= bound {
			h.buckets[i]++
			return
		}
	}
	// Last bucket for > max bound
	h.buckets[len(h.buckets)-1]++
}

// Stats returns histogram statistics
func (h *LatencyHistogram) Stats() HistogramStats {
	h.mu.Lock()
	buckets := make([]int64, len(h.buckets))
	copy(buckets, h.buckets)
	h.mu.Unlock()

	count := h.count.Load()
	sum := h.sum.Load()

	var avg time.Duration
	if count > 0 {
		avg = time.Duration(sum / count)
	}

	return HistogramStats{
		Count:   count,
		Sum:     time.Duration(sum),
		Average: avg,
		Buckets: buckets,
	}
}

// HistogramStats contains histogram statistics
type HistogramStats struct {
	Count   int64
	Sum     time.Duration
	Average time.Duration
	Buckets []int64
}

// New creates a new Metrics instance
func New() *Metrics {
	return &Metrics{
		requestCounters:   make(map[string]*atomic.Int64),
		errorCounters:     make(map[string]map[string]*atomic.Int64),
		latencyHistograms: make(map[string]*LatencyHistogram),
		tokenCounters:     make(map[string]*TokenCounter),
		activeRequests:    make(map[string]*atomic.Int64),
	}
}

// RecordRequest records a completed request
func (m *Metrics) RecordRequest(provider, model, status string, durationSeconds float64) {
	m.mu.Lock()
	counter, exists := m.requestCounters[provider]
	if !exists {
		counter = &atomic.Int64{}
		m.requestCounters[provider] = counter
	}

	histogram, histExists := m.latencyHistograms[provider]
	if !histExists {
		histogram = NewLatencyHistogram()
		m.latencyHistograms[provider] = histogram
	}
	m.mu.Unlock()

	counter.Add(1)
	histogram.Observe(time.Duration(durationSeconds * float64(time.Second)))

	// Record error if not success
	if status != "success" && status != "200" && status != "" {
		m.RecordError(provider, status)
	}
}

// RecordError records an error
func (m *Metrics) RecordError(provider, errorType string) {
	m.mu.Lock()
	providerErrors, exists := m.errorCounters[provider]
	if !exists {
		providerErrors = make(map[string]*atomic.Int64)
		m.errorCounters[provider] = providerErrors
	}

	counter, counterExists := providerErrors[errorType]
	if !counterExists {
		counter = &atomic.Int64{}
		providerErrors[errorType] = counter
	}
	m.mu.Unlock()

	counter.Add(1)
}

// RecordTokens records token usage
func (m *Metrics) RecordTokens(provider string, prompt, completion int) {
	m.mu.Lock()
	counter, exists := m.tokenCounters[provider]
	if !exists {
		counter = &TokenCounter{}
		m.tokenCounters[provider] = counter
	}
	m.mu.Unlock()

	counter.PromptTokens.Add(int64(prompt))
	counter.CompletionTokens.Add(int64(completion))
	counter.TotalTokens.Add(int64(prompt + completion))
}

// IncActiveRequests increments active request count
func (m *Metrics) IncActiveRequests(provider string) {
	m.mu.Lock()
	counter, exists := m.activeRequests[provider]
	if !exists {
		counter = &atomic.Int64{}
		m.activeRequests[provider] = counter
	}
	m.mu.Unlock()

	counter.Add(1)
}

// DecActiveRequests decrements active request count
func (m *Metrics) DecActiveRequests(provider string) {
	m.mu.RLock()
	counter, exists := m.activeRequests[provider]
	m.mu.RUnlock()

	if exists {
		counter.Add(-1)
	}
}

// GetStats returns current metrics statistics
func (m *Metrics) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := Stats{
		Providers: make(map[string]ProviderStats),
	}

	for provider, counter := range m.requestCounters {
		ps := ProviderStats{
			TotalRequests: counter.Load(),
		}

		if histogram, exists := m.latencyHistograms[provider]; exists {
			ps.Latency = histogram.Stats()
		}

		if tokens, exists := m.tokenCounters[provider]; exists {
			ps.Tokens = TokenStats{
				Prompt:     tokens.PromptTokens.Load(),
				Completion: tokens.CompletionTokens.Load(),
				Total:      tokens.TotalTokens.Load(),
			}
		}

		if active, exists := m.activeRequests[provider]; exists {
			ps.ActiveRequests = active.Load()
		}

		if errors, exists := m.errorCounters[provider]; exists {
			ps.Errors = make(map[string]int64)
			for errType, errCounter := range errors {
				ps.Errors[errType] = errCounter.Load()
			}
		}

		stats.Providers[provider] = ps
	}

	return stats
}

// Stats contains aggregated metrics
type Stats struct {
	Providers map[string]ProviderStats
}

// ProviderStats contains per-provider metrics
type ProviderStats struct {
	TotalRequests  int64
	ActiveRequests int64
	Latency        HistogramStats
	Tokens         TokenStats
	Errors         map[string]int64
}

// TokenStats contains token usage statistics
type TokenStats struct {
	Prompt     int64
	Completion int64
	Total      int64
}

// Reset resets all metrics (useful for testing)
func (m *Metrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requestCounters = make(map[string]*atomic.Int64)
	m.errorCounters = make(map[string]map[string]*atomic.Int64)
	m.latencyHistograms = make(map[string]*LatencyHistogram)
	m.tokenCounters = make(map[string]*TokenCounter)
	m.activeRequests = make(map[string]*atomic.Int64)
}
