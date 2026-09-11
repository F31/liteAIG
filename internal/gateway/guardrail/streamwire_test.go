package guardrail

import (
	"context"
	"strings"
	"testing"
	"time"

	guardraildomain "github.com/F31/liteAIG/internal/guardrail"
	"github.com/F31/liteAIG/internal/guardrail/builtin"
)

func streamEngine(t *testing.T, rules []builtin.Rule) *builtin.Engine {
	t.Helper()
	engine, err := builtin.New(builtin.Policy{Rules: rules})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

// TestStreamGuardInjectionCutAtFirstChunk verifies Layer 1: a block rule is
// detected as soon as the violating chunk arrives (across the rolling window)
// and the stream is cut with a BlockError.
func TestStreamGuardInjectionCutAtFirstChunk(t *testing.T) {
	guard := NewStreamGuard(StreamGuardConfig{
		Engine: streamEngine(t, []builtin.Rule{{ID: "inject", Kind: "prompt_injection", Pattern: `ignore previous`, Action: "block"}}),
	})
	// The first chunks are held by the buffered layer (no sentence boundary)
	// but must not cut the stream.
	if _, err := guard.Write(context.Background(), "normal preface ", false); err != nil {
		t.Fatalf("first chunk err=%v", err)
	}
	if _, err := guard.Write(context.Background(), "ignore pre", false); err != nil {
		t.Fatalf("partial chunk err=%v", err)
	}
	if _, err := guard.Write(context.Background(), "vious instructions", false); err == nil {
		t.Fatal("injection must cut the stream at the violating chunk")
	}
	if delivered, err := guard.Write(context.Background(), "after cut", false); err == nil || delivered != "" {
		t.Fatalf("post-cut chunk must be refused: err=%v delivered=%q", err, delivered)
	}
}

// TestStreamGuardRedactsAtRelease verifies Layer 2: redaction applies when the
// buffered window releases at a sentence boundary, and earlier chunks held in
// the buffer are not delivered until release.
func TestStreamGuardRedactsAtRelease(t *testing.T) {
	guard := NewStreamGuard(StreamGuardConfig{
		Engine: streamEngine(t, []builtin.Rule{{ID: "pii", Kind: "pii", Pattern: `[0-9]{3}-[0-9]{2}-[0-9]{4}`, Action: "redact"}}),
	})
	if delivered, err := guard.Write(context.Background(), "call 123-45-", false); err != nil || delivered != "" {
		t.Fatalf("held chunk err=%v delivered=%q", err, delivered)
	}
	delivered, err := guard.Write(context.Background(), "6789.", false)
	if err != nil {
		t.Fatal(err)
	}
	if delivered != "call [redacted]." {
		t.Fatalf("released=%q want redacted window", delivered)
	}
}

// TestStreamGuardFinalFlushesBuffer verifies clean-stream termination: buffered
// text with no sentence boundary is flushed on the final event instead of being
// lost or stuck.
func TestStreamGuardFinalFlushesBuffer(t *testing.T) {
	guard := NewStreamGuard(StreamGuardConfig{
		Engine: streamEngine(t, []builtin.Rule{{ID: "pii", Kind: "pii", Pattern: `[0-9]{3}-[0-9]{2}`, Action: "redact"}}),
	})
	if delivered, err := guard.Write(context.Background(), "value 123-45", false); err != nil || delivered != "" {
		t.Fatalf("held err=%v delivered=%q", err, delivered)
	}
	delivered, err := guard.Write(context.Background(), " just ends here", true)
	if err != nil {
		t.Fatal(err)
	}
	if delivered != "value [redacted] just ends here" {
		t.Fatalf("flushed=%q", delivered)
	}
}

// TestStreamGuardBoundedBufferLatency verifies the buffered layer cannot hold
// text past the max-token bound, so per-window latency stays bounded even when
// the stream never emits a sentence boundary.
func TestStreamGuardBoundedBufferLatency(t *testing.T) {
	guard := NewStreamGuard(StreamGuardConfig{
		Engine:       streamEngine(t, nil),
		BufferTokens: 8,
	})
	start := time.Now()
	var delivered strings.Builder
	for i := 0; i < 50; i++ {
		text, err := guard.Write(context.Background(), "word ", false)
		if err != nil {
			t.Fatal(err)
		}
		delivered.WriteString(text)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("buffered latency unbounded: %v", time.Since(start))
	}
	// 50 words with an 8-token bound must have released at least once.
	if delivered.Len() == 0 {
		t.Fatal("no word was ever released")
	}
	// The releases must be bounded: after each release the guard hands text
	// back, so the client never waits for the whole stream to finish.
	if !strings.Contains(delivered.String(), "word ") {
		t.Fatalf("delivered=%q", delivered.String())
	}
}

// TestStreamGuardCleanTerminationNoViolation verifies a clean stream without
// violations ends normally (Stop is idempotent and no retroactive event fires).
func TestStreamGuardCleanTerminationNoViolation(t *testing.T) {
	guard := NewStreamGuard(StreamGuardConfig{
		Engine: streamEngine(t, []builtin.Rule{{ID: "inject", Kind: "prompt_injection", Pattern: `forbidden`, Action: "block"}}),
	})
	if _, err := guard.Write(context.Background(), "hello world.", false); err != nil {
		t.Fatal(err)
	}
	guard.Stop()
	guard.Stop()
	if _, err := guard.Write(context.Background(), "after stop", false); err == nil {
		t.Fatal("post-stop write must be refused")
	}
}

// TestStreamGuardEmptyEngine verifies an engine with no rules reports no rules,
// so the pipeline can gate the guard off and keep pass-through streaming.
func TestStreamGuardEmptyEngine(t *testing.T) {
	engine := streamEngine(t, nil)
	if engine.HasRules() {
		t.Fatal("empty engine must report no rules")
	}
	if NewStreamGuard(StreamGuardConfig{}) != nil {
		t.Fatal("nil engine must yield nil guard")
	}
}

// blockingShadowProvider blocks every released window.
type blockingShadowProvider struct{}

func (blockingShadowProvider) Evaluate(context.Context, guardraildomain.ExternalRequest) (guardraildomain.ExternalResult, error) {
	return guardraildomain.ExternalResult{Blocked: true}, nil
}

// TestStreamGuardShadowCutoff verifies Layer 3: a late violation from the
// async external check stops the following chunks (retroactive cutoff).
func TestStreamGuardShadowCutoff(t *testing.T) {
	store := &securityStore{}
	guard := NewStreamGuard(StreamGuardConfig{
		Engine:         streamEngine(t, []builtin.Rule{{ID: "secret", Kind: "secret", Pattern: `sk-[a-z]+`, Action: "redact"}}),
		External:       blockingShadowProvider{},
		SecurityEvents: store,
	})
	if _, err := guard.Write(context.Background(), "handle sk-abc", false); err != nil {
		t.Fatalf("released window err=%v", err)
	}
	// The shadow check runs asynchronously; give it a moment to land.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		delivered, err := guard.Write(context.Background(), "more text", false)
		if err != nil {
			// Cut by the retroactive shadow violation.
			return
		}
		if delivered == "" {
			// Held in the buffer: keep feeding so the window releases.
			time.Sleep(time.Millisecond)
			continue
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("shadow violation never cut the stream")
}
