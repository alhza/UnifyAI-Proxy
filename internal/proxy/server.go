package proxy

import (
	"net/http"
)

// Server holds HTTP server wiring. Implement using gin/nethttp as needed.
type Server struct {
	httpServer *http.Server
}

// NewServer creates a new server with middleware and routes wired.
func NewServer() *Server {
	// TODO: init router, middleware, routes
	return &Server{}
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	if s.httpServer == nil {
		// TODO: assign actual http.Server
		return nil
	}
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown() error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Close()
}
