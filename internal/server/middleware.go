package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RequestIDKey is the context key for request ID
type RequestIDKey struct{}

// LoggingMiddleware logs incoming requests
func LoggingMiddleware(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			
			// Wrap response writer to capture status code
			wrapper := &responseWrapper{ResponseWriter: w, status: http.StatusOK}
			
			next.ServeHTTP(wrapper, r)
			
			duration := time.Since(start)
			
			logger.Info("request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", wrapper.status),
				slog.Duration("duration", duration),
				slog.String("remote_addr", r.RemoteAddr),
			)
		})
	}
}

// responseWrapper wraps http.ResponseWriter to capture status code
type responseWrapper struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *responseWrapper) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
		w.ResponseWriter.WriteHeader(code)
	}
}

func (w *responseWrapper) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// RecoveryMiddleware recovers from panics
func RecoveryMiddleware(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					logger.Error("panic recovered",
						slog.Any("error", err),
						slog.String("path", r.URL.Path),
					)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// CORSMiddleware handles CORS headers
func CORSMiddleware(allowedOrigins []string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			
			// Check if origin is allowed
			allowed := false
			for _, o := range allowedOrigins {
				if o == "*" || o == origin {
					allowed = true
					break
				}
			}
			
			if allowed && origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
				w.Header().Set("Access-Control-Max-Age", "86400")
			}
			
			// Handle preflight
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			
			next.ServeHTTP(w, r)
		})
	}
}

// AuthMiddleware validates API key authentication using HMAC-SHA256 hashing
// to prevent timing attacks (no length-based short-circuit)
func AuthMiddleware(apiKeys []string) Middleware {
	// Pre-compute HMAC-SHA256 hashes of all valid keys using a fixed secret
	// This prevents timing attacks based on key length differences
	hashSecret := []byte("unifyai-proxy-auth-v1")
	keyHashes := make([][]byte, len(apiKeys))
	for i, k := range apiKeys {
		keyHashes[i] = computeKeyHash(hashSecret, k)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for health/ready endpoints
			if r.URL.Path == "/health" || r.URL.Path == "/ready" {
				next.ServeHTTP(w, r)
				return
			}

			// Get API key from header
			authHeader := r.Header.Get("Authorization")
			var apiKey string

			if strings.HasPrefix(authHeader, "Bearer ") {
				apiKey = strings.TrimPrefix(authHeader, "Bearer ")
			} else {
				apiKey = r.Header.Get("X-API-Key")
			}

			if apiKey == "" {
				WriteError(w, http.StatusUnauthorized, "invalid_api_key", "Invalid or missing API key")
				return
			}

			// Compute hash of provided key and compare against all valid hashes
			// This is constant-time regardless of key lengths
			providedHash := computeKeyHash(hashSecret, apiKey)
			valid := false
			for _, kh := range keyHashes {
				// hmac.Equal is constant-time for equal-length inputs (which hashes always are)
				if hmac.Equal(providedHash, kh) {
					valid = true
					// Don't break early to maintain constant time across all keys
				}
			}

			if !valid {
				WriteError(w, http.StatusUnauthorized, "invalid_api_key", "Invalid or missing API key")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// computeKeyHash computes HMAC-SHA256 of a key using the provided secret
func computeKeyHash(secret []byte, key string) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(key))
	return h.Sum(nil)
}

// RateLimiter provides rate limiting middleware with cleanup capability
type RateLimiter struct {
	store      *rateLimiterStore
	middleware Middleware
}

// Middleware returns the rate limiting middleware handler
func (rl *RateLimiter) Middleware() Middleware {
	return rl.middleware
}

// Stop stops the background cleanup goroutine
func (rl *RateLimiter) Stop() {
	rl.store.stop()
}

// NewRateLimiter creates a new rate limiter with cleanup capability.
// Call Stop() when shutting down to prevent goroutine leak.
func NewRateLimiter(requestsPerSecond float64, burstSize int) *RateLimiter {
	if requestsPerSecond <= 0 {
		requestsPerSecond = 10 // Default: 10 requests per second
	}
	if burstSize <= 0 {
		burstSize = 20 // Default: burst of 20
	}

	// Use a map to track rate limiters per client
	store := &rateLimiterStore{
		limiters: make(map[string]*rate.Limiter),
		rate:     rate.Limit(requestsPerSecond),
		burst:    burstSize,
		stopCh:   make(chan struct{}),
	}

	// Cleanup old limiters periodically (prevent memory leak)
	go store.cleanupLoop()

	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip rate limiting for health/ready endpoints
			if r.URL.Path == "/health" || r.URL.Path == "/ready" {
				next.ServeHTTP(w, r)
				return
			}

			// Get client identifier (prefer API key, fallback to IP)
			clientID := getClientIdentifier(r)
			limiter := store.get(clientID)

			if !limiter.Allow() {
				w.Header().Set("Retry-After", "1")
				WriteError(w, http.StatusTooManyRequests, "rate_limited", "Rate limit exceeded. Please slow down your requests.")
				return
			}

			next.ServeHTTP(w, r)
		})
	}

	return &RateLimiter{
		store:      store,
		middleware: middleware,
	}
}

// RateLimitMiddleware implements token bucket rate limiting per client (by API key or IP)
// Deprecated: Use NewRateLimiter instead for proper cleanup support.
// This function is kept for backward compatibility but the cleanup goroutine
// will run for the lifetime of the process.
func RateLimitMiddleware(requestsPerSecond float64, burstSize int) Middleware {
	rl := NewRateLimiter(requestsPerSecond, burstSize)
	return rl.Middleware()
}

// rateLimiterStore manages per-client rate limiters with thread-safe access
type rateLimiterStore struct {
	limiters map[string]*rate.Limiter
	rate     rate.Limit
	burst    int
	mu       sync.RWMutex
	lastSeen map[string]time.Time
	stopCh   chan struct{}  // Channel to signal cleanup goroutine to stop
	stopped  bool           // Flag to prevent double-stop
}

// get returns or creates a rate limiter for the given client ID
func (s *rateLimiterStore) get(clientID string) *rate.Limiter {
	s.mu.RLock()
	limiter, exists := s.limiters[clientID]
	s.mu.RUnlock()

	if exists {
		s.updateLastSeen(clientID)
		return limiter
	}

	// Create new limiter
	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock
	if limiter, exists = s.limiters[clientID]; exists {
		return limiter
	}

	limiter = rate.NewLimiter(s.rate, s.burst)
	s.limiters[clientID] = limiter
	if s.lastSeen == nil {
		s.lastSeen = make(map[string]time.Time)
	}
	s.lastSeen[clientID] = time.Now()
	return limiter
}

// updateLastSeen updates the last seen time for a client
func (s *rateLimiterStore) updateLastSeen(clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastSeen == nil {
		s.lastSeen = make(map[string]time.Time)
	}
	s.lastSeen[clientID] = time.Now()
}

// cleanupLoop removes stale rate limiters to prevent memory leaks
// It listens for the stop signal to gracefully exit
func (s *rateLimiterStore) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.cleanup()
		case <-s.stopCh:
			return
		}
	}
}

// stop signals the cleanup goroutine to stop
func (s *rateLimiterStore) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.stopped && s.stopCh != nil {
		close(s.stopCh)
		s.stopped = true
	}
}

// cleanup removes limiters not seen in the last 10 minutes
func (s *rateLimiterStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	threshold := time.Now().Add(-10 * time.Minute)
	for clientID, lastSeen := range s.lastSeen {
		if lastSeen.Before(threshold) {
			delete(s.limiters, clientID)
			delete(s.lastSeen, clientID)
		}
	}
}

// getClientIdentifier extracts client identifier from request (API key or IP)
func getClientIdentifier(r *http.Request) string {
	// Try to get API key first
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return "key:" + strings.TrimPrefix(authHeader, "Bearer ")
	}
	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		return "key:" + apiKey
	}

	// Fall back to IP address
	// Check X-Forwarded-For for proxied requests
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the chain
		if idx := strings.Index(xff, ","); idx != -1 {
			return "ip:" + strings.TrimSpace(xff[:idx])
		}
		return "ip:" + strings.TrimSpace(xff)
	}

	// Check X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return "ip:" + xri
	}

	// Fall back to RemoteAddr
	return "ip:" + r.RemoteAddr
}

// TimeoutMiddleware adds request timeout
func TimeoutMiddleware(timeout time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

