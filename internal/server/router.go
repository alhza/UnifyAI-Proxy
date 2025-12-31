package server

import (
	"net/http"
	"strings"
	"sync"
)

// Router is a simple HTTP router
type Router struct {
	routes     map[string]map[string]http.HandlerFunc // method -> path -> handler
	middleware []Middleware
	notFound   http.HandlerFunc
	mu         sync.RWMutex
}

// NewRouter creates a new router
func NewRouter() *Router {
	return &Router{
		routes:     make(map[string]map[string]http.HandlerFunc),
		middleware: make([]Middleware, 0),
		notFound:   defaultNotFound,
	}
}

// Use adds middleware to the router
func (r *Router) Use(mw ...Middleware) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.middleware = append(r.middleware, mw...)
}

// Handle registers a handler for the given method and path
func (r *Router) Handle(method, path string, handler http.HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.routes[method] == nil {
		r.routes[method] = make(map[string]http.HandlerFunc)
	}
	r.routes[method][path] = handler
}

// GET registers a GET handler
func (r *Router) GET(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodGet, path, handler)
}

// POST registers a POST handler
func (r *Router) POST(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodPost, path, handler)
}

// PUT registers a PUT handler
func (r *Router) PUT(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodPut, path, handler)
}

// DELETE registers a DELETE handler
func (r *Router) DELETE(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodDelete, path, handler)
}

// OPTIONS registers an OPTIONS handler
func (r *Router) OPTIONS(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodOptions, path, handler)
}

// NotFound sets the not found handler
func (r *Router) NotFound(handler http.HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notFound = handler
}

// ServeHTTP implements http.Handler
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.RLock()
	methodRoutes, ok := r.routes[req.Method]
	if !ok {
		r.mu.RUnlock()
		r.notFound(w, req)
		return
	}

	// Try exact match first
	handler, ok := methodRoutes[req.URL.Path]
	if ok {
		r.mu.RUnlock()
		r.applyMiddleware(handler).ServeHTTP(w, req)
		return
	}

	// Try prefix match for wildcard routes
	for path, h := range methodRoutes {
		if strings.HasSuffix(path, "*") {
			prefix := strings.TrimSuffix(path, "*")
			if strings.HasPrefix(req.URL.Path, prefix) {
				r.mu.RUnlock()
				r.applyMiddleware(h).ServeHTTP(w, req)
				return
			}
		}
	}

	r.mu.RUnlock()
	r.notFound(w, req)
}

// applyMiddleware applies middleware to a handler
func (r *Router) applyMiddleware(handler http.HandlerFunc) http.Handler {
	var h http.Handler = handler
	for i := len(r.middleware) - 1; i >= 0; i-- {
		h = r.middleware[i](h)
	}
	return h
}

// defaultNotFound is the default 404 handler
func defaultNotFound(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Not Found", http.StatusNotFound)
}

// Group creates a route group with a common prefix
type Group struct {
	router     *Router
	prefix     string
	middleware []Middleware
}

// Group creates a new route group
func (r *Router) Group(prefix string) *Group {
	return &Group{
		router:     r,
		prefix:     prefix,
		middleware: make([]Middleware, 0),
	}
}

// Use adds middleware to the group
func (g *Group) Use(mw ...Middleware) *Group {
	g.middleware = append(g.middleware, mw...)
	return g
}

// Handle registers a handler for the group
func (g *Group) Handle(method, path string, handler http.HandlerFunc) {
	fullPath := g.prefix + path
	wrappedHandler := g.wrapHandler(handler)
	g.router.Handle(method, fullPath, wrappedHandler)
}

// wrapHandler wraps a handler with group middleware
func (g *Group) wrapHandler(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var h http.Handler = handler
		for i := len(g.middleware) - 1; i >= 0; i-- {
			h = g.middleware[i](h)
		}
		h.ServeHTTP(w, r)
	}
}

// GET registers a GET handler for the group
func (g *Group) GET(path string, handler http.HandlerFunc) {
	g.Handle(http.MethodGet, path, handler)
}

// POST registers a POST handler for the group
func (g *Group) POST(path string, handler http.HandlerFunc) {
	g.Handle(http.MethodPost, path, handler)
}
