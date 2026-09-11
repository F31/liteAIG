package guardrail

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/F31/liteAIG/internal/guardrail/builtin"
)

// BlockError is returned when a streaming guard cuts the output stream. It
// carries a stable code so the pipeline can classify it as non-retryable.
type BlockError struct {
	Message string
}

func (e *BlockError) Error() string {
	if e.Message == "" {
		return "stream blocked by guardrail"
	}
	return e.Message
}

var ErrStreamBlocked = errors.New("stream blocked by guardrail")

// InlineStreamGuard is Layer 1: local checks with a rolling cross-chunk window.
type InlineStreamGuard struct {
	mu          sync.Mutex
	engine      *builtin.Engine
	window      string
	windowBytes int
}

func NewInlineStreamGuard(engine *builtin.Engine, windowBytes int) *InlineStreamGuard {
	if windowBytes <= 0 {
		windowBytes = 256
	}
	return &InlineStreamGuard{engine: engine, windowBytes: windowBytes}
}

func (g *InlineStreamGuard) Evaluate(chunk string) builtin.Result {
	g.mu.Lock()
	defer g.mu.Unlock()
	combined := g.window + chunk
	result := g.engine.Evaluate(combined)
	if len(combined) > g.windowBytes {
		g.window = combined[len(combined)-g.windowBytes:]
	} else {
		g.window = combined
	}
	return result
}

// BufferedStreamGuard is Layer 2: release at sentence/newline/token boundaries.
type BufferedStreamGuard struct {
	engine    *builtin.Engine
	buffer    strings.Builder
	maxTokens int
}

func NewBufferedStreamGuard(engine *builtin.Engine, maxTokens int) *BufferedStreamGuard {
	if maxTokens <= 0 {
		maxTokens = 64
	}
	return &BufferedStreamGuard{engine: engine, maxTokens: maxTokens}
}

// Push returns release=true when a local window is complete.
func (g *BufferedStreamGuard) Push(chunk string) (result builtin.Result, release bool) {
	g.buffer.WriteString(chunk)
	value := g.buffer.String()
	release = strings.ContainsAny(chunk, ".!?\n") || len(strings.Fields(value)) >= g.maxTokens
	if !release {
		return builtin.Result{}, false
	}
	result = g.engine.Evaluate(value)
	g.buffer.Reset()
	return result, true
}

// Flush evaluates and resets the pending buffer without waiting for a
// boundary. It is used at end-of-stream so no buffered text is lost.
func (g *BufferedStreamGuard) Flush() builtin.Result {
	value := g.buffer.String()
	g.buffer.Reset()
	if value == "" {
		return builtin.Result{}
	}
	return g.engine.Evaluate(value)
}

// ShadowGuard is Layer 3: it evaluates already-released content against an
// External Guardrail asynchronously, never on the delivery path. A late
// violation stops further output and reports a retroactive disposition.
type ShadowGuard struct {
	provider ExternalProvider
	observe  func(retroactive bool)
	mu       sync.Mutex
	stopped  bool
	stop     chan struct{}
}

// NewShadowGuard builds a Layer 3 guard. observe reports violations; when it
// returns, the stop channel is closed and future output is refused.
func NewShadowGuard(provider ExternalProvider, observe func(retroactive bool)) *ShadowGuard {
	return &ShadowGuard{provider: provider, observe: observe, stop: make(chan struct{})}
}

// Submit sends already-released content for async evaluation. It never blocks
// the caller on the provider.
func (g *ShadowGuard) Submit(ctx context.Context, request ExternalRequest, released string) {
	go func() {
		if request.ContentHash == "" {
			request.ContentHash = "shadow:" + released
		}
		result, err := EvaluateExternal(ctx, g.provider, request, ExternalFailOpen, nil)
		if err != nil || !result.Blocked {
			return
		}
		g.mu.Lock()
		alreadyStopped := g.stopped
		g.stopped = true
		g.mu.Unlock()
		if !alreadyStopped {
			close(g.stop)
		}
		if g.observe != nil {
			g.observe(true)
		}
	}()
}

// Stop signals that the stream ended without a violation.
func (g *ShadowGuard) Stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.stopped {
		g.stopped = true
		close(g.stop)
	}
}

// Allow reports whether further output may be delivered.
func (g *ShadowGuard) Allow() bool {
	select {
	case <-g.stop:
		return false
	default:
		return true
	}
}
