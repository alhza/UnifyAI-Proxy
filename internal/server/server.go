package server

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/unifyai-proxy/unifyai-proxy/internal/config"
)

// Server represents the HTTP server
type Server struct {
	config     *config.Config
	httpServer *http.Server
	router     *Router
	middleware []Middleware
	mu         sync.RWMutex
	running    bool
}

// Option configures the server
type Option func(*Server)

// WithMiddleware adds middleware to the server
func WithMiddleware(mw ...Middleware) Option {
	return func(s *Server) {
		s.middleware = append(s.middleware, mw...)
	}
}

// New creates a new server instance
func New(cfg *config.Config, opts ...Option) *Server {
	s := &Server{
		config:     cfg,
		router:     NewRouter(),
		middleware: make([]Middleware, 0),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Router returns the server's router
func (s *Server) Router() *Router {
	return s.router
}

// Start starts the HTTP server
func (s *Server) Start() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("server is already running")
	}

	// Build handler with middleware
	var handler http.Handler = s.router
	for i := len(s.middleware) - 1; i >= 0; i-- {
		handler = s.middleware[i](handler)
	}

	addr := fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.Port)
	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadTimeout:       time.Duration(s.config.Server.ReadTimeout) * time.Second,
		WriteTimeout:      time.Duration(s.config.Server.WriteTimeout) * time.Second,
		IdleTimeout:       time.Duration(s.config.Server.IdleTimeout) * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
	}

	s.running = true
	s.mu.Unlock()

	// Start server
	if s.config.TLS.Enable {
		return s.httpServer.ListenAndServeTLS(
			s.config.TLS.CertFile,
			s.config.TLS.KeyFile,
		)
	}

	return s.httpServer.ListenAndServe()
}

// Stop gracefully stops the server
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.running = false
	return s.httpServer.Shutdown(ctx)
}

// IsRunning returns true if the server is running
func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// Addr returns the server address
func (s *Server) Addr() string {
	return fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.Port)
}

// Middleware is a function that wraps an http.Handler
type Middleware func(http.Handler) http.Handler

// MiddlewareFunc is a convenience type for creating middleware
type MiddlewareFunc func(http.Handler) http.Handler

// Chain chains multiple middleware together
func Chain(middlewares ...Middleware) Middleware {
	return func(next http.Handler) http.Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return next
	}
}

