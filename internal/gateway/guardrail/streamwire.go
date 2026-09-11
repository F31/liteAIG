// Three-tier streaming guard wiring (spec §16): streaming responses run a
// rolling Inline fast guard on every chunk, a Buffered sentence-boundary
// release with local redaction, and an asynchronous Shadow check with stream
// cutoff on late violations. These guards only police the text delta; tool-call
// JSON fragments are governed by the fixed output checkpoint on the assembled
// response.
package guardrail

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"

	guardraildomain "github.com/F31/liteAIG/internal/guardrail"
	"github.com/F31/liteAIG/internal/guardrail/builtin"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/tenancy"
)

// StreamGuardConfig wires the three streaming tiers to a request scope.
type StreamGuardConfig struct {
	Engine          *builtin.Engine
	WindowBytes     int // Layer 1 rolling window (0 = default 256)
	BufferTokens    int // Layer 2 release bound (0 = default 64)
	External        guardraildomain.ExternalProvider
	SecurityEvents  guardraildomain.SecurityEventStore
	Sink            contracts.EventSink
	IDs             contracts.IDGenerator
	Clock           contracts.Clock
	TenantID        string
	ProjectID       string
	RequestID       string
	SnapshotVersion int64
}

// StreamGuard applies the three tiers to each chunk delta. It is driven by a
// single stream reader and is not safe for concurrent use.
type StreamGuard struct {
	inline   *guardraildomain.InlineStreamGuard
	buffered *guardraildomain.BufferedStreamGuard
	shadow   *guardraildomain.ShadowGuard
	cfg      StreamGuardConfig
	stopped  bool
}

// NewStreamGuard composes the three tiers. A zero External provider disables
// Layer 3 without changing Layer 1/2 behavior.
func NewStreamGuard(cfg StreamGuardConfig) *StreamGuard {
	if cfg.Engine == nil {
		return nil
	}
	guard := &StreamGuard{
		inline:   guardraildomain.NewInlineStreamGuard(cfg.Engine, cfg.WindowBytes),
		buffered: guardraildomain.NewBufferedStreamGuard(cfg.Engine, cfg.BufferTokens),
		cfg:      cfg,
	}
	if cfg.External != nil {
		guard.shadow = guardraildomain.NewShadowGuard(cfg.External, guard.reportRetroactive)
	}
	return guard
}

// Write processes one text delta. It returns the text that may be delivered to
// the client (possibly redacted or empty while the buffered layer holds it).
// A non-nil BlockError means the stream must be cut: an inline block fired, or
// a late shadow violation stopped further output. final flushes the buffered
// window so end-of-stream text is not lost.
func (g *StreamGuard) Write(ctx context.Context, delta string, final bool) (string, error) {
	if g == nil || g.stopped {
		return "", &guardraildomain.BlockError{Message: "stream stopped"}
	}
	// Layer 3: a late violation already cut the stream.
	if g.shadow != nil && !g.shadow.Allow() {
		g.stopped = true
		return "", &guardraildomain.BlockError{Message: "stream cut by retroactive guardrail"}
	}
	// Layer 1: rolling inline fast guard on the fresh text. A block matches the
	// combined window (past + current chunk) and cuts immediately.
	if delta != "" {
		inlineResult := g.inline.Evaluate(delta)
		if inlineResult.Blocked {
			g.emitMatches(ctx, inlineResult)
			g.stopped = true
			return "", &guardraildomain.BlockError{Message: "stream blocked by inline guardrail"}
		}
	}
	// Layer 2: buffer until a sentence/newline/token boundary. Local redaction
	// applies at release; the redacted window is delivered in one release.
	result, release := g.buffered.Push(delta)
	if final {
		result = g.buffered.Flush()
		if len(result.Matches) > 0 {
			g.emitMatches(ctx, result)
		}
		if result.Content != "" {
			g.submitReleased(ctx, result.Content)
		}
		return result.Content, nil
	}
	if !release {
		return "", nil
	}
	if len(result.Matches) > 0 {
		g.emitMatches(ctx, result)
	}
	g.submitReleased(ctx, result.Content)
	return result.Content, nil
}

// Stop signals a clean end of stream: the shadow guard releases its evaluation
// window without treating normal termination as a violation, and later writes
// are refused.
func (g *StreamGuard) Stop() {
	if g == nil {
		return
	}
	g.stopped = true
	if g.shadow != nil {
		g.shadow.Stop()
	}
}

// submitReleased hands released content to the async Layer 3 check.
func (g *StreamGuard) submitReleased(ctx context.Context, released string) {
	if g.shadow == nil || released == "" {
		return
	}
	sum := sha256.Sum256([]byte(released))
	g.shadow.Submit(ctx, guardraildomain.ExternalRequest{
		TenantID: g.cfg.TenantID, ProjectID: g.cfg.ProjectID, RequestID: g.cfg.RequestID,
		ContentHash: hex.EncodeToString(sum[:]),
	}, released)
}

// reportRetroactive records a late Layer 3 violation as a security event with
// Action "retroactive". It runs on the shadow goroutine, so it uses a
// background context.
func (g *StreamGuard) reportRetroactive(retroactive bool) {
	if !retroactive {
		return
	}
	g.emit(context.Background(), "", "guardrail.retroactive", map[string]string{
		"action": "retroactive", "snapshot_version": strconv.FormatInt(g.cfg.SnapshotVersion, 10),
	})
}

// emitMatches records Layer 1/2 matches through the configured security event
// store and event sink.
func (g *StreamGuard) emitMatches(ctx context.Context, result builtin.Result) {
	for _, match := range result.Matches {
		g.emit(ctx, match.RuleID, "guardrail.match", map[string]string{
			"rule_id": match.RuleID, "action": match.Action, "content_hash": match.ContentHash,
		})
	}
}

func (g *StreamGuard) emit(ctx context.Context, ruleID, kind string, attributes map[string]string) {
	if g.cfg.SecurityEvents == nil && g.cfg.Sink == nil {
		return
	}
	if g.cfg.IDs == nil || g.cfg.Clock == nil {
		return
	}
	id, _ := g.cfg.IDs.New()
	now := g.cfg.Clock.Now()
	if g.cfg.Sink != nil {
		_ = g.cfg.Sink.Emit(ctx, contracts.DomainEvent{
			ID: id, Kind: kind, OccurredAt: now,
			TenantID: g.cfg.TenantID, ProjectID: g.cfg.ProjectID, RequestID: g.cfg.RequestID,
			Attributes: attributes,
		})
	}
	if g.cfg.SecurityEvents != nil {
		_ = g.cfg.SecurityEvents.Create(ctx, tenancy.TenantScope{TenantID: g.cfg.TenantID}, guardraildomain.SecurityEvent{
			ID: id, TenantID: g.cfg.TenantID, ProjectID: g.cfg.ProjectID, RequestID: g.cfg.RequestID,
			PolicyID: "stream", RuleID: ruleID, Action: attributes["action"],
			ContentHash: attributes["content_hash"], SnapshotVersion: g.cfg.SnapshotVersion, OccurredAt: now,
		})
	}
}
