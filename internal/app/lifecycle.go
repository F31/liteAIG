// Package app composes the control and gateway planes into one process,
// including the graceful drain lifecycle.
package app

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/F31/liteAIG/internal/platform/webkit"
)

// Phase is the lifecycle state of a runtime process.
type Phase int32

const (
	// PhaseRunning accepts traffic.
	PhaseRunning Phase = iota
	// PhaseDraining stops new traffic and completes in-flight work.
	PhaseDraining
	// PhaseExited means the process is about to exit.
	PhaseExited
)

// Default drain timeouts used when no explicit configuration is provided.
const (
	DefaultDrainTimeout         = 30 * time.Second
	DefaultStreamDrainTimeout   = 60 * time.Second
	DefaultForceShutdownTimeout = 70 * time.Second
)

// DrainConfig governs graceful shutdown behavior.
type DrainConfig struct {
	DrainTimeout         time.Duration // ordinary in-flight requests
	StreamDrainTimeout   time.Duration // SSE/A2A/MCP long streams
	ForceShutdownTimeout time.Duration // hard cap on the entire drain phase
}

// WithDefaults fills zero-valued fields with the documented defaults.
func (c DrainConfig) WithDefaults() DrainConfig {
	if c.DrainTimeout <= 0 {
		c.DrainTimeout = DefaultDrainTimeout
	}
	if c.StreamDrainTimeout <= 0 {
		c.StreamDrainTimeout = DefaultStreamDrainTimeout
	}
	if c.ForceShutdownTimeout <= 0 {
		c.ForceShutdownTimeout = DefaultForceShutdownTimeout
	}
	return c
}

// ValidateDrainConfig rejects invalid drain timeout combinations.
func ValidateDrainConfig(c DrainConfig) error {
	if c.DrainTimeout < 0 || c.StreamDrainTimeout < 0 || c.ForceShutdownTimeout < 0 {
		return errors.New("drain timeouts must be non-negative")
	}
	c = c.WithDefaults()
	if c.DrainTimeout > c.ForceShutdownTimeout {
		return errors.New("drain_timeout must not exceed force_shutdown_timeout")
	}
	if c.StreamDrainTimeout > c.ForceShutdownTimeout {
		return errors.New("stream_drain_timeout must not exceed force_shutdown_timeout")
	}
	return nil
}

// StreamRegistry tracks active long-lived streams and refuses new ones while draining.
type StreamRegistry struct {
	mu       sync.Mutex
	streams  map[string]struct{}
	draining bool
	next     int64
}

// NewStreamRegistry creates an empty stream registry.
func NewStreamRegistry() *StreamRegistry {
	return &StreamRegistry{streams: map[string]struct{}{}}
}

// Register tracks a new long stream; returns ok=false once draining begins.
func (r *StreamRegistry) Register() (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.draining {
		return "", false
	}
	r.next++
	id := time.Now().Format("150405.000000000") + "-" + string(rune('a'+r.next%26))
	r.streams[id] = struct{}{}
	return id, true
}

// Unregister removes a completed long stream.
func (r *StreamRegistry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.streams, id)
}

// Count returns the number of active long streams.
func (r *StreamRegistry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.streams)
}

// BeginDrain stops accepting new long streams.
func (r *StreamRegistry) BeginDrain() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.draining = true
}

// Wait blocks until no streams are active or the context is done.
func (r *StreamRegistry) Wait(ctx context.Context) error {
	for {
		if r.Count() == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// Lifecycle implements the RUNNING → DRAINING → EXIT state machine.
type Lifecycle struct {
	phase    atomic.Int32
	config   DrainConfig
	streams  *StreamRegistry
	inflight sync.WaitGroup
	gateMu   sync.Mutex // serializes Acquire against BeginDrain's wait start
	onDrain  func(context.Context) error
}

// NewLifecycle builds a drain lifecycle. onDrain finalizes global state such as
// flushing the accounting spool before exit and may be nil.
func NewLifecycle(config DrainConfig, streams *StreamRegistry, onDrain func(context.Context) error) (*Lifecycle, error) {
	if err := ValidateDrainConfig(config); err != nil {
		return nil, err
	}
	config = config.WithDefaults()
	if streams == nil {
		streams = NewStreamRegistry()
	}
	return &Lifecycle{config: config, streams: streams, onDrain: onDrain}, nil
}

// Running reports whether the process is accepting traffic.
func (l *Lifecycle) Running() bool {
	return Phase(l.phase.Load()) == PhaseRunning
}

// Ready reports whether readiness probes should return healthy.
func (l *Lifecycle) Ready() bool {
	return Phase(l.phase.Load()) == PhaseRunning
}

// Streams exposes the long-stream registry for adapter registration.
func (l *Lifecycle) Streams() *StreamRegistry { return l.streams }

// AddInflight registers an in-flight request.
func (l *Lifecycle) AddInflight() { l.inflight.Add(1) }

// DoneInflight marks an in-flight request complete.
func (l *Lifecycle) DoneInflight() { l.inflight.Done() }

// Acquire atomically decides whether a new request may start and registers it
// in-flight. It refuses once the lifecycle has left RUNNING. BeginDrain holds
// the same gate before starting its in-flight wait, so an accepted request is
// always counted before the wait can observe an empty group (no WaitGroup
// counter race, no request slipping past the drain).
func (l *Lifecycle) Acquire() bool {
	l.gateMu.Lock()
	defer l.gateMu.Unlock()
	if Phase(l.phase.Load()) != PhaseRunning {
		return false
	}
	l.inflight.Add(1)
	return true
}

// SetDrainFinalizer installs (or replaces) the drain finalizer on a lifecycle
// created by an outer composition root, e.g. the spool flush wired by Lite.
func (l *Lifecycle) SetDrainFinalizer(fn func(context.Context) error) { l.onDrain = fn }

// BeginDrain transitions to DRAINING, waits for in-flight requests and long
// streams, runs the drain finalizer, and marks the process exited. The caller
// provides a context that is cancelled at force-shutdown time.
func (l *Lifecycle) BeginDrain(ctx context.Context) error {
	if !l.phase.CompareAndSwap(int32(PhaseRunning), int32(PhaseDraining)) {
		return nil
	}
	// Hold the gate so no Acquire is between its RUNNING check and its
	// Add(1): every accepted request is counted before the wait starts, and
	// any later Acquire sees DRAINING and refuses.
	l.gateMu.Lock()
	l.streams.BeginDrain()

	requestCtx, cancelRequests := context.WithTimeout(ctx, l.config.DrainTimeout)
	defer cancelRequests()
	waitDone := make(chan struct{})
	go func() {
		l.inflight.Wait()
		close(waitDone)
	}()
	l.gateMu.Unlock()
	select {
	case <-waitDone:
	case <-requestCtx.Done():
	}

	streamCtx, cancelStreams := context.WithTimeout(ctx, l.config.StreamDrainTimeout)
	defer cancelStreams()
	_ = l.streams.Wait(streamCtx)

	if l.onDrain != nil {
		_ = l.onDrain(ctx)
	}
	l.phase.Store(int32(PhaseExited))
	return ctx.Err()
}

// WaitForSignal blocks until a termination signal is received, then begins the
// drain sequence bounded by force_shutdown_timeout.
func (l *Lifecycle) WaitForSignal(ctx context.Context, signals <-chan struct{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-signals:
	}
	forceCtx, cancel := context.WithTimeout(context.Background(), l.config.ForceShutdownTimeout)
	defer cancel()
	return l.BeginDrain(forceCtx)
}

// drainGate is an engine middleware: once the lifecycle leaves RUNNING it
// refuses new traffic with the drain error contract, and every accepted
// request is counted in-flight so BeginDrain can wait for outstanding work.
func drainGate(lifecycle *Lifecycle) webkit.Middleware {
	return func(next webkit.Handler) webkit.Handler {
		return func(c *webkit.Context) error {
			if !lifecycle.Acquire() {
				w := c.Response()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":{"code":"DRAINING","message":"server is draining"}}`))
				return nil
			}
			defer lifecycle.DoneInflight()
			return next(c)
		}
	}
}
