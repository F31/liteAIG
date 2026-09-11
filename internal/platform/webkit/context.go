package webkit

import (
	"encoding/json"
	"io"
	"net/http"
)

// Context carries one request through the middleware chain: request/response
// access, path parameters, handler values, and JSON helpers.
type Context struct {
	r      *http.Request
	w      *responseWriter
	values map[string]any
}

func newContext(w http.ResponseWriter, r *http.Request) *Context {
	wrapped, ok := w.(*responseWriter)
	if !ok {
		wrapped = &responseWriter{ResponseWriter: w}
	}
	return &Context{r: r, w: wrapped}
}

// Request returns the underlying request.
func (c *Context) Request() *http.Request { return c.r }

// Response returns the wrapped response writer.
func (c *Context) Response() http.ResponseWriter { return c.w }

// Header returns the response header (usable until the first write).
func (c *Context) Header() http.Header { return c.w.Header() }

// Param returns a path parameter value ("GET /keys/{id}" -> Param("id")).
func (c *Context) Param(name string) string { return c.r.PathValue(name) }

// Path returns the request URL path.
func (c *Context) Path() string { return c.r.URL.Path }

// Written reports whether any response byte or status has been emitted.
func (c *Context) Written() bool { return c.w.written }

// Set stores a request-scoped value (e.g. the resolved session).
func (c *Context) Set(key string, value any) {
	if c.values == nil {
		c.values = make(map[string]any)
	}
	c.values[key] = value
}

// Get retrieves a request-scoped value.
func (c *Context) Get(key string) (any, bool) {
	value, ok := c.values[key]
	return value, ok
}

// Status writes the HTTP status code (idempotent once written).
func (c *Context) Status(code int) { c.w.WriteHeader(code) }

// JSON writes a JSON response with the given status.
func (c *Context) JSON(status int, value any) error {
	c.w.Header().Set("Content-Type", "application/json")
	c.w.WriteHeader(status)
	return json.NewEncoder(c.w).Encode(value)
}

// Text writes a plain-text response with the given status.
func (c *Context) Text(status int, body string) error {
	c.w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.w.WriteHeader(status)
	_, err := io.WriteString(c.w, body)
	return err
}

// Bind decodes the JSON request body into v, bounding its size when
// maxBytes > 0. A size-exceeded or malformed body yields an error.
func (c *Context) Bind(v any, maxBytes int64) error {
	var body io.Reader = c.r.Body
	if maxBytes > 0 {
		body = http.MaxBytesReader(c.w.ResponseWriter, c.r.Body, maxBytes)
	}
	return json.NewDecoder(body).Decode(v)
}

// responseWriter tracks whether the response has started so the engine can
// avoid double-writing after a post-write error.
type responseWriter struct {
	http.ResponseWriter
	written bool
}

func (w *responseWriter) WriteHeader(code int) {
	if !w.written {
		w.written = true
		w.ResponseWriter.WriteHeader(code)
	}
}

func (w *responseWriter) Write(b []byte) (int, error) {
	w.written = true
	return w.ResponseWriter.Write(b)
}

func (w *responseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
