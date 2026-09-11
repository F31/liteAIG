package app

import (
	"net/http"
	"time"

	"github.com/F31/liteAIG/internal/platform/webkit"
)

// ReadyFunc reports whether the process is ready to serve traffic.
type ReadyFunc func() bool

// ReadinessHandler serves /readyz (not-ready while draining) and /healthz
// (process liveness) for load balancer and orchestration probes.
func ReadinessHandler(ready ReadyFunc) http.Handler {
	e := webkit.New()
	e.Handle("GET /readyz", func(c *webkit.Context) error {
		if ready != nil && ready() {
			return c.Text(http.StatusOK, "ready")
		}
		return c.Text(http.StatusServiceUnavailable, "not ready")
	})
	e.Handle("GET /healthz", func(c *webkit.Context) error {
		return c.Text(http.StatusOK, "ok")
	})
	return e
}

// NewReadinessServer builds an HTTP server exposing readiness for the
// lifecycle. If runtimeReady is non-nil, /readyz additionally requires it to
// report true, which lets the process declare that the data plane has at
// least one loaded tenant runtime before accepting traffic.
func NewReadinessServer(addr string, lifecycle *Lifecycle, runtimeReady func() bool) *http.Server {
	ready := lifecycle.Ready
	if runtimeReady != nil {
		ready = func() bool { return lifecycle.Ready() && runtimeReady() }
	}
	return &http.Server{
		Addr:              addr,
		Handler:           ReadinessHandler(ready),
		ReadHeaderTimeout: 5 * time.Second,
	}
}
