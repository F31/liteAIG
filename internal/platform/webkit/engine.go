// Package webkit is a lightweight, dependency-free HTTP microkernel for the
// LiteAIG planes.
//
// It borrows the core ideas of mature frameworks (Echo, chi) without adding
// a third-party dependency or any business logic:
//
//   - routing delegates to Go 1.22+ net/http.ServeMux (method + {param}
//     patterns, specificity matching);
//   - a thin Context wraps request/response, path params, and JSON binding;
//   - middleware composes in onion order at engine, group, and route level;
//   - handlers return errors that a pluggable ErrorHandler renders, keeping
//     domain error-to-HTTP mapping outside the kernel.
//
// Domain packages compose Engines, mount route groups as plugins, and inject
// their own error handlers and middleware. The kernel itself stays
// business-free.
package webkit

import (
	"log"
	"net/http"
	"strings"
)

// Handler serves one request. Returning a non-nil error routes it to the
// engine's ErrorHandler unless the handler already wrote a response.
type Handler func(c *Context) error

// Middleware wraps the next handler (onion order).
type Middleware func(next Handler) Handler

// ErrorHandler renders an unhandled error as the HTTP response.
type ErrorHandler func(c *Context, err error)

// Engine is a microkernel: routing + middleware chain + error contract.
type Engine struct {
	mux         *http.ServeMux
	middlewares []Middleware
	errHandler  ErrorHandler
	logf        func(string, ...any)
}

// New creates an engine with the default error handler (JSON error body) and
// the standard library logger.
func New() *Engine {
	return &Engine{
		mux:        http.NewServeMux(),
		errHandler: defaultErrorHandler,
		logf:       log.Printf,
	}
}

// Use appends engine-wide middleware applied to every route.
func (e *Engine) Use(mw ...Middleware) *Engine {
	e.middlewares = append(e.middlewares, mw...)
	return e
}

// ErrorHandler installs the error renderer. Domain engines pass their own
// mapping (protocol-specific error shapes, logging policy).
func (e *Engine) ErrorHandler(h ErrorHandler) {
	if h != nil {
		e.errHandler = h
	}
}

// Logger overrides the logger used by Recover and the default error handler.
func (e *Engine) Logger(logf func(string, ...any)) {
	if logf != nil {
		e.logf = logf
	}
}

// ServeHTTP implements http.Handler so an engine can be mounted directly on
// a parent mux (e.g. root.Handle("/api/admin/", adminEngine)).
func (e *Engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.mux.ServeHTTP(w, r)
}

// Handle registers a route with optional route-level middleware. The pattern
// is a Go 1.22 ServeMux pattern, e.g. "GET /api/admin/keys/{id}".
func (e *Engine) Handle(pattern string, h Handler, mw ...Middleware) {
	e.register(pattern, h, nil, mw)
}

// Group starts a route group (plugin mount point) sharing a path prefix and
// middleware across a set of routes.
func (e *Engine) Group(prefix string, mw ...Middleware) *Group {
	return &Group{engine: e, prefix: prefix, middlewares: append([]Middleware(nil), mw...)}
}

// HandleHTTP mounts a foreign http.Handler (another plane, a file server, a
// legacy handler) under a route pattern.
func (e *Engine) HandleHTTP(pattern string, h http.Handler) {
	e.Handle(pattern, FromHandler(h))
}

// FromHandler adapts a foreign http.Handler to a webkit route handler. The
// wrapped handler owns the response, so a nil error is returned once it
// completes.
func FromHandler(h http.Handler) Handler {
	return func(c *Context) error {
		h.ServeHTTP(c.Response(), c.Request())
		return nil
	}
}

// register wires one route: global middleware, then group middleware, then
// route middleware, then the handler (onion order).
func (e *Engine) register(pattern string, h Handler, groupMW, routeMW []Middleware) {
	e.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		c := newContext(w, r)
		chain := h
		for i := len(routeMW) - 1; i >= 0; i-- {
			chain = routeMW[i](chain)
		}
		for i := len(groupMW) - 1; i >= 0; i-- {
			chain = groupMW[i](chain)
		}
		for i := len(e.middlewares) - 1; i >= 0; i-- {
			chain = e.middlewares[i](chain)
		}
		if err := chain(c); err != nil {
			e.finish(c, err)
		}
	})
}

// finish renders the terminal error unless the response was already started.
// Post-write errors (mid-stream failures) are logged, never double-written.
func (e *Engine) finish(c *Context, err error) {
	if c.Written() {
		e.logf("webkit: handler error after response started: %v", err)
		return
	}
	e.errHandler(c, err)
}

// Group shares a path prefix and middleware across a set of routes.
type Group struct {
	engine      *Engine
	prefix      string
	middlewares []Middleware
}

// Use appends group middleware to every route registered on this group.
func (g *Group) Use(mw ...Middleware) *Group {
	g.middlewares = append(g.middlewares, mw...)
	return g
}

// Handle registers a route under the group prefix.
func (g *Group) Handle(pattern string, h Handler, mw ...Middleware) {
	g.engine.register(joinPattern(g.prefix, pattern), h, g.middlewares, mw)
}

// joinPattern merges a group prefix ("/api/admin") with a route pattern
// ("GET /keys" or "/keys") into a ServeMux pattern.
func joinPattern(prefix, pattern string) string {
	parts := strings.SplitN(pattern, " ", 2)
	if len(parts) == 2 {
		if prefix == "" {
			return pattern
		}
		return parts[0] + " " + prefix + parts[1]
	}
	return prefix + pattern
}
