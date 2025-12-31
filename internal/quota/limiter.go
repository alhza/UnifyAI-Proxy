package quota

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/unifyai-proxy/unifyai-proxy/internal/account"
)

// QuotaState represents the current quota status
// Reference: docs/unifyai_proxy/15-任务拆解-基础设计.md
type QuotaState int

const (
	// QuotaStateOK indicates quota is within limits
	QuotaStateOK QuotaState = iota
	// QuotaStateNearLimit indicates approaching limit (>80%)
	QuotaStateNearLimit
	// QuotaStateExceeded indicates quota has been exceeded
	QuotaStateExceeded
	// QuotaStateRateLimited indicates rate limit (RPM) triggered
	QuotaStateRateLimited
)

// String returns the string representation of QuotaState
func (s QuotaState) String() string {
	switch s {
	case QuotaStateOK:
		return "ok"
	case QuotaStateNearLimit:
		return "near_limit"
	case QuotaStateExceeded:
		return "exceeded"
	case QuotaStateRateLimited:
		return "rate_limited"
	default:
		return "unknown"
	}
}

// Common errors
var (
	ErrRateLimited    = errors.New("rate limited")
	ErrQuotaExceeded  = errors.New("quota exceeded")
	ErrAccountBlocked = errors.New("account blocked")
)

// RateLimiter controls request rate
type RateLimiter interface {
	// Allow checks if a request is allowed for the given key
	Allow(ctx context.Context, key string) error
	// AllowN checks if N requests are allowed
	AllowN(ctx context.Context, key string, n int) error
	// Reset resets the rate limiter for a key
	Reset(key string)
}

// RateLimiterWithCount extends RateLimiter with count inspection capabilities
type RateLimiterWithCount interface {
	RateLimiter
	// CurrentCount returns the current request count for a key
	CurrentCount(key string) int
	// Limit returns the configured rate limit
	Limit() int
}

// QuotaCounter tracks and enforces quota limits
type QuotaCounter interface {
	// Add adds usage to the account and returns the new state
	Add(acc *account.Account, reqs, tokens int64) QuotaState
	// Check checks the current quota state without adding usage
	Check(acc *account.Account) QuotaState
	// Reset resets quota counters for daily reset
	Reset(now time.Time)
	// GetUsage returns current usage for an account
	GetUsage(acc *account.Account) (requests, tokens int64)
}

// SlidingWindowRateLimiter implements rate limiting with sliding window
type SlidingWindowRateLimiter struct {
	windows       map[string]*slidingWindow
	mu            sync.RWMutex
	limit         int           // requests per window
	window        time.Duration // window duration
	cleanupTicker *time.Ticker
	stopCleanup   chan struct{}
	stopOnce      sync.Once // Prevents double-close panic on stopCleanup channel
}

type slidingWindow struct {
	requests   []time.Time
	lastAccess time.Time
	mu         sync.Mutex
}

// NewSlidingWindowRateLimiter creates a new sliding window rate limiter
func NewSlidingWindowRateLimiter(limit int, window time.Duration) *SlidingWindowRateLimiter {
	r := &SlidingWindowRateLimiter{
		windows:     make(map[string]*slidingWindow),
		limit:       limit,
		window:      window,
		stopCleanup: make(chan struct{}),
	}
	// Start cleanup goroutine - run every 5 minutes
	r.cleanupTicker = time.NewTicker(5 * time.Minute)
	go r.cleanupLoop()
	return r
}

// cleanupLoop periodically removes stale windows to prevent memory leaks
func (r *SlidingWindowRateLimiter) cleanupLoop() {
	for {
		select {
		case <-r.stopCleanup:
			return
		case <-r.cleanupTicker.C:
			r.cleanup()
		}
	}
}

// cleanup removes windows that haven't been accessed for more than 2x the window duration
func (r *SlidingWindowRateLimiter) cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	staleThreshold := r.window * 2
	if staleThreshold < time.Minute {
		staleThreshold = time.Minute
	}

	// Collect keys to delete first, then delete
	// This avoids holding window lock while modifying the map
	keysToDelete := make([]string, 0)
	for key, w := range r.windows {
		w.mu.Lock()
		isStale := now.Sub(w.lastAccess) > staleThreshold
		w.mu.Unlock()
		if isStale {
			keysToDelete = append(keysToDelete, key)
		}
	}

	for _, key := range keysToDelete {
		delete(r.windows, key)
	}
}

// Stop stops the cleanup goroutine and releases resources.
// Safe to call multiple times - uses sync.Once internally to prevent double-close panic.
func (r *SlidingWindowRateLimiter) Stop() {
	r.stopOnce.Do(func() {
		if r.cleanupTicker != nil {
			r.cleanupTicker.Stop()
		}
		close(r.stopCleanup)
	})
}

// CurrentCount returns the current request count (implements RateLimiterWithCount)
func (r *SlidingWindowRateLimiter) CurrentCount(key string) int {
	return r.GetCurrentCount(key)
}

// Limit returns the rate limit (implements RateLimiterWithCount)
func (r *SlidingWindowRateLimiter) Limit() int {
	return r.limit
}

// Allow checks if a request is allowed
func (r *SlidingWindowRateLimiter) Allow(ctx context.Context, key string) error {
	return r.AllowN(ctx, key, 1)
}

// AllowN checks if N requests are allowed
func (r *SlidingWindowRateLimiter) AllowN(ctx context.Context, key string, n int) error {
	now := time.Now()

	r.mu.Lock()
	w, exists := r.windows[key]
	if !exists {
		w = &slidingWindow{
			requests:   make([]time.Time, 0),
			lastAccess: now,
		}
		r.windows[key] = w
	}
	r.mu.Unlock()

	w.mu.Lock()
	defer w.mu.Unlock()

	// Update last access time for cleanup tracking
	w.lastAccess = now
	windowStart := now.Add(-r.window)

	// Remove expired requests
	validRequests := make([]time.Time, 0, len(w.requests))
	for _, t := range w.requests {
		if t.After(windowStart) {
			validRequests = append(validRequests, t)
		}
	}
	w.requests = validRequests

	// Check if we can add n requests
	if len(w.requests)+n > r.limit {
		return ErrRateLimited
	}

	// Add new requests
	for i := 0; i < n; i++ {
		w.requests = append(w.requests, now)
	}

	return nil
}

// Reset resets the rate limiter for a key
func (r *SlidingWindowRateLimiter) Reset(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.windows, key)
}

// GetCurrentCount returns the current request count for a key
func (r *SlidingWindowRateLimiter) GetCurrentCount(key string) int {
	r.mu.RLock()
	w, exists := r.windows[key]
	r.mu.RUnlock()

	if !exists {
		return 0
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-r.window)

	count := 0
	for _, t := range w.requests {
		if t.After(windowStart) {
			count++
		}
	}

	return count
}


// DefaultQuotaCounter implements quota tracking and enforcement
type DefaultQuotaCounter struct {
	nearLimitThreshold float64 // Percentage threshold for near limit (e.g., 0.8 for 80%)
}

// NewDefaultQuotaCounter creates a new quota counter
func NewDefaultQuotaCounter() *DefaultQuotaCounter {
	return &DefaultQuotaCounter{
		nearLimitThreshold: 0.8,
	}
}

// Add adds usage to the account and returns the new quota state
func (q *DefaultQuotaCounter) Add(acc *account.Account, reqs, tokens int64) QuotaState {
	// Record usage on the account
	acc.RecordUsage(reqs, tokens)
	return q.Check(acc)
}

// Check checks the current quota state without adding usage
func (q *DefaultQuotaCounter) Check(acc *account.Account) QuotaState {
	usage := acc.GetUsage()

	// Check if quota is exceeded
	if acc.DailyRequestLimit > 0 && usage.Requests >= acc.DailyRequestLimit {
		return QuotaStateExceeded
	}
	if acc.DailyTokenLimit > 0 && usage.Tokens >= acc.DailyTokenLimit {
		return QuotaStateExceeded
	}

	// Check RPM limit
	if acc.RPMLimit > 0 && usage.RPMRequests >= int64(acc.RPMLimit) {
		return QuotaStateRateLimited
	}

	// Check if near limit
	if acc.DailyRequestLimit > 0 {
		ratio := float64(usage.Requests) / float64(acc.DailyRequestLimit)
		if ratio >= q.nearLimitThreshold {
			return QuotaStateNearLimit
		}
	}
	if acc.DailyTokenLimit > 0 {
		ratio := float64(usage.Tokens) / float64(acc.DailyTokenLimit)
		if ratio >= q.nearLimitThreshold {
			return QuotaStateNearLimit
		}
	}

	return QuotaStateOK
}

// Reset resets quota counters for daily reset
func (q *DefaultQuotaCounter) Reset(now time.Time) {
	// This is typically called by a background goroutine
	// Individual account resets are handled by the account itself
}

// GetUsage returns current usage for an account
func (q *DefaultQuotaCounter) GetUsage(acc *account.Account) (requests, tokens int64) {
	usage := acc.GetUsage()
	return usage.Requests, usage.Tokens
}

// QuotaManager manages quota across multiple accounts
type QuotaManager struct {
	rateLimiter  RateLimiter
	quotaCounter QuotaCounter
	mu           sync.RWMutex
}

// NewQuotaManager creates a new quota manager
func NewQuotaManager(rateLimiter RateLimiter, quotaCounter QuotaCounter) *QuotaManager {
	return &QuotaManager{
		rateLimiter:  rateLimiter,
		quotaCounter: quotaCounter,
	}
}

// CheckAndRecord checks quota and records usage
func (m *QuotaManager) CheckAndRecord(ctx context.Context, acc *account.Account, tokens int64) (QuotaState, error) {
	// Check rate limit first
	if m.rateLimiter != nil {
		key := acc.Provider + ":" + acc.ID
		if err := m.rateLimiter.Allow(ctx, key); err != nil {
			return QuotaStateRateLimited, err
		}
	}

	// Check and record quota
	state := m.quotaCounter.Add(acc, 1, tokens)
	if state == QuotaStateExceeded {
		return state, ErrQuotaExceeded
	}

	return state, nil
}

// PreCheck checks if a request can be made without recording usage
func (m *QuotaManager) PreCheck(ctx context.Context, acc *account.Account) (QuotaState, error) {
	// Check rate limit using the interface abstraction
	if m.rateLimiter != nil {
		key := acc.Provider + ":" + acc.ID
		// Try to use the extended interface if available
		if rlc, ok := m.rateLimiter.(RateLimiterWithCount); ok {
			count := rlc.CurrentCount(key)
			if count >= rlc.Limit() {
				return QuotaStateRateLimited, ErrRateLimited
			}
		}
		// If not RateLimiterWithCount, we can't pre-check rate limit
		// The actual check will happen in CheckAndRecord
	}

	// Check quota
	state := m.quotaCounter.Check(acc)
	if state == QuotaStateExceeded {
		return state, ErrQuotaExceeded
	}

	return state, nil
}

// GetQuotaInfo returns quota information for an account
func (m *QuotaManager) GetQuotaInfo(acc *account.Account) QuotaInfo {
	requests, tokens := m.quotaCounter.GetUsage(acc)

	return QuotaInfo{
		AccountID:         acc.ID,
		Provider:          acc.Provider,
		RequestsUsed:      requests,
		RequestsLimit:     acc.DailyRequestLimit,
		TokensUsed:        tokens,
		TokensLimit:       acc.DailyTokenLimit,
		RPMUsed:           int(acc.GetUsage().RPMRequests),
		RPMLimit:          acc.RPMLimit,
		State:             m.quotaCounter.Check(acc),
		ResetAt:           acc.ResetAt,
	}
}

// QuotaInfo contains quota information for display
type QuotaInfo struct {
	AccountID     string
	Provider      string
	RequestsUsed  int64
	RequestsLimit int64
	TokensUsed    int64
	TokensLimit   int64
	RPMUsed       int
	RPMLimit      int
	State         QuotaState
	ResetAt       time.Time
}

// PercentageUsed returns the percentage of quota used
func (q QuotaInfo) PercentageUsed() float64 {
	if q.RequestsLimit == 0 && q.TokensLimit == 0 {
		return 0
	}

	var maxPct float64

	if q.RequestsLimit > 0 {
		pct := float64(q.RequestsUsed) / float64(q.RequestsLimit) * 100
		if pct > maxPct {
			maxPct = pct
		}
	}

	if q.TokensLimit > 0 {
		pct := float64(q.TokensUsed) / float64(q.TokensLimit) * 100
		if pct > maxPct {
			maxPct = pct
		}
	}

	return maxPct
}
