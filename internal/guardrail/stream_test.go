package guardrail

import (
	"context"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/guardrail/builtin"
)

func TestInlineStreamGuardDetectsAcrossChunks(t *testing.T) {
	engine, err := builtin.New(builtin.Policy{Rules: []builtin.Rule{{ID: "inject", Kind: "prompt_injection", Pattern: `ignore previous`, Action: "block"}}})
	if err != nil {
		t.Fatal(err)
	}
	guard := NewInlineStreamGuard(engine, 64)
	if guard.Evaluate("ignore pre").Blocked {
		t.Fatal("first partial chunk blocked")
	}
	if !guard.Evaluate("vious instructions").Blocked {
		t.Fatal("cross-chunk phrase not blocked")
	}
}

func TestInlineStreamGuardLatencyBudget(t *testing.T) {
	engine, _ := builtin.New(builtin.Policy{Rules: []builtin.Rule{{ID: "secret", Kind: "secret", Pattern: `sk-[a-z]+`, Action: "redact"}}})
	guard := NewInlineStreamGuard(engine, 256)
	const samples = 10_000
	start := time.Now()
	for i := 0; i < samples; i++ {
		guard.Evaluate("ordinary stream chunk")
	}
	perChunk := time.Since(start) / samples
	if perChunk > 300*time.Microsecond {
		t.Fatalf("Layer 1 latency = %v/chunk, budget 300us", perChunk)
	}
}

func TestShadowGuardRetroactiveBlockStopsOutput(t *testing.T) {
	provider := blockingProvider{result: ExternalResult{Blocked: true}}
	guard := NewShadowGuard(provider, func(retroactive bool) {
		if !retroactive {
			t.Fatal("violation must be reported as retroactive")
		}
	})
	guard.Submit(context.Background(), ExternalRequest{ContentHash: "h"}, "already-released")
	deadline := time.Now().Add(time.Second)
	for guard.Allow() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if guard.Allow() {
		t.Fatal("shadow guard did not stop output after violation")
	}
	// Stop is idempotent.
	guard.Stop()
	guard.Stop()
}

type blockingProvider struct{ result ExternalResult }

func (p blockingProvider) Evaluate(context.Context, ExternalRequest) (ExternalResult, error) {
	return p.result, nil
}

func TestBufferedStreamGuardReleasesAtBoundary(t *testing.T) {
	engine, _ := builtin.New(builtin.Policy{Rules: []builtin.Rule{{ID: "pii", Kind: "pii", Pattern: `[0-9]{3}-[0-9]{2}`, Action: "redact"}}})
	guard := NewBufferedStreamGuard(engine, 64)
	if _, release := guard.Push("value 123-"); release {
		t.Fatal("released before boundary")
	}
	result, release := guard.Push("45.")
	if !release || result.Content != "value [redacted]." {
		t.Fatalf("buffered result = %+v release=%t", result, release)
	}
}
