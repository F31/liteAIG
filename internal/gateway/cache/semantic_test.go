package cache

import (
	"context"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/cache"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/pipeline"
	"github.com/F31/liteAIG/internal/kernel/runtime"
)

// keywordEmbedder returns a stable topic vector based on whether the input
// mentions a keyword, so semantically similar prompts share a vector while
// unrelated prompts differ. Deterministic for tests.
type keywordEmbedder struct{ keyword string }

func (e keywordEmbedder) Embed(_ context.Context, input string, _ int) ([]float64, error) {
	if containsKeyword(input, e.keyword) {
		return []float64{1, 0, 0, 0}, nil
	}
	return []float64{0, 1, 0, 0}, nil
}

func containsKeyword(input, keyword string) bool {
	for i := 0; i+len(keyword) <= len(input); i++ {
		if input[i:i+len(keyword)] == keyword {
			return true
		}
	}
	return false
}

func semanticSnapshot(policy runtime.CachePolicy) *runtime.TenantRuntimeSnapshot {
	return runtime.NewTenantSnapshot(runtime.TenantSnapshotData{TenantID: "tenant", TenantRef: "ref", Version: 2, Cache: policy})
}

func semanticRequest(content string, tool bool) *kernel.RequestContext {
	messages := []interaction.Message{{Role: "user", Content: content}}
	if tool {
		messages = append(messages, interaction.Message{Role: "tool", Content: `{"tool_call":1}`})
	}
	return &kernel.RequestContext{RequestID: "r", Snapshot: semanticSnapshot(runtime.CachePolicy{Enabled: true, NamespaceVersion: 1}), Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "chat", Chat: &interaction.ChatPayload{Messages: messages}}, Interaction: &interaction.Context{TenantID: "tenant", ProjectID: "project"}}
}

func TestSemanticHitShortCircuitsWithSource(t *testing.T) {
	ctx := context.Background()
	semanticStore := cache.NewMemorySemanticStore()
	exactStore := cache.NewMemoryStore(cache.MemoryConfig{Capacity: 8})
	handler := SemanticHandler{Store: semanticStore, Exact: exactStore, Embedder: keywordEmbedder{keyword: "refund"}}

	// First request: miss → vector indexed, response cached via writeback.
	first := semanticRequest("what is the refund policy", false)
	directive, err := handler.Handle(ctx, first)
	if err != nil || directive != pipeline.Continue || !first.CachePending {
		t.Fatalf("first handle = %v, %v", directive, err)
	}
	first.Response = &interaction.UnifiedResponse{ID: "upstream", Choices: []interaction.Choice{{Message: interaction.Message{Role: "assistant", Content: "refund within 30 days"}}}}
	if err := WriteBackSemantic(ctx, exactStore, first, time.Minute); err != nil {
		t.Fatal(err)
	}

	// A semantically equivalent request is a hit.
	second := semanticRequest("what is the refund policy?", false)
	directive, err = handler.Handle(ctx, second)
	if err != nil || directive != pipeline.SkipResolutionExecution || !second.CacheHit {
		t.Fatalf("semantic hit = %v, %v (hit=%t)", directive, err, second.CacheHit)
	}
	if second.Source != "semantic_cache" || second.Response == nil {
		t.Fatalf("hit source = %q response=%+v", second.Source, second.Response)
	}
}

func TestSemanticToolCallNotCached(t *testing.T) {
	ctx := context.Background()
	handler := SemanticHandler{Store: cache.NewMemorySemanticStore(), Exact: cache.NewMemoryStore(cache.MemoryConfig{Capacity: 4}), Embedder: keywordEmbedder{keyword: "refund"}}
	request := semanticRequest("tool call", true)
	directive, err := handler.Handle(ctx, request)
	if err != nil || directive != pipeline.Continue || request.CachePending {
		t.Fatalf("tool call must not be cached: %v %v", directive, err)
	}
}

func TestSemanticSecurityBlockedNotServed(t *testing.T) {
	ctx := context.Background()
	store := cache.NewMemorySemanticStore()
	exact := cache.NewMemoryStore(cache.MemoryConfig{Capacity: 4})
	handler := SemanticHandler{Store: store, Exact: exact, Embedder: keywordEmbedder{keyword: "refund"}, SecurityBlocked: func(*kernel.RequestContext) bool { return true }}
	request := semanticRequest("sensitive content", false)
	directive, err := handler.Handle(ctx, request)
	if err != nil || directive != pipeline.Continue || request.CachePending {
		t.Fatalf("security-blocked request must not be cached: %v %v", directive, err)
	}
}

func TestSemanticNoCacheBypass(t *testing.T) {
	ctx := context.Background()
	handler := SemanticHandler{Store: cache.NewMemorySemanticStore(), Exact: cache.NewMemoryStore(cache.MemoryConfig{Capacity: 4}), Embedder: keywordEmbedder{keyword: "refund"}}
	request := semanticRequest("no cache please", false)
	request.Request.Metadata = map[string]string{"cache-control": "no-cache"}
	directive, err := handler.Handle(ctx, request)
	if err != nil || directive != pipeline.Continue || request.CachePending {
		t.Fatalf("no-cache must bypass semantic cache: %v %v", directive, err)
	}
}

func TestSemanticHitStillRunsOutputGuardrailAndAccounting(t *testing.T) {
	ctx := context.Background()
	semanticStore := cache.NewMemorySemanticStore()
	exactStore := cache.NewMemoryStore(cache.MemoryConfig{Capacity: 8})
	semanticHandler := SemanticHandler{Store: semanticStore, Exact: exactStore, Embedder: keywordEmbedder{keyword: "help"}}

	var executionCalls, outputCalls, accountingCalls int
	runner, err := pipeline.NewRunner(pipeline.Handlers{
		Admission: pipeline.HandlerFunc(func(context.Context, *kernel.RequestContext) (pipeline.Directive, error) {
			return pipeline.Continue, nil
		}),
		InputGuardrail: pipeline.HandlerFunc(func(context.Context, *kernel.RequestContext) (pipeline.Directive, error) {
			return pipeline.Continue, nil
		}),
		PolicyCostPreflight: semanticHandler,
		Resolution: pipeline.HandlerFunc(func(context.Context, *kernel.RequestContext) (pipeline.Directive, error) {
			return pipeline.Continue, nil
		}),
		ExecutionResilience: pipeline.HandlerFunc(func(context.Context, *kernel.RequestContext) (pipeline.Directive, error) {
			executionCalls++
			return pipeline.Continue, nil
		}),
		OutputStreamGuardrail: pipeline.HandlerFunc(func(context.Context, *kernel.RequestContext) (pipeline.Directive, error) {
			outputCalls++
			return pipeline.Continue, nil
		}),
		AccountingAndTelemetry: pipeline.HandlerFunc(func(context.Context, *kernel.RequestContext) (pipeline.Directive, error) {
			accountingCalls++
			return pipeline.Continue, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	first := semanticRequest("help me", false)
	if err := runner.Run(ctx, first); err != nil {
		t.Fatal(err)
	}
	first.Response = &interaction.UnifiedResponse{ID: "upstream", Choices: []interaction.Choice{{Message: interaction.Message{Role: "assistant", Content: "ok"}}}}
	if err := WriteBackSemantic(ctx, exactStore, first, time.Minute); err != nil {
		t.Fatal(err)
	}
	second := semanticRequest("help me please", false)
	if err := runner.Run(ctx, second); err != nil {
		t.Fatal(err)
	}
	if !second.CacheHit {
		t.Fatal("second request was not a semantic hit")
	}
	if executionCalls != 1 {
		t.Fatalf("execution ran on semantic hit: %d", executionCalls)
	}
	if outputCalls != 2 || accountingCalls != 2 {
		t.Fatalf("output=%d accounting=%d, want 2/2", outputCalls, accountingCalls)
	}
}

func TestSemanticMetricsReachSink(t *testing.T) {
	ctx := context.Background()
	metrics := &recordingMetrics{}
	handler := SemanticHandler{
		Store: cache.NewMemorySemanticStore(), Exact: cache.NewMemoryStore(cache.MemoryConfig{Capacity: 8}),
		Embedder: keywordEmbedder{keyword: "help"}, Metrics: metrics,
	}
	first := semanticRequest("help me", false)
	_, _ = handler.Handle(ctx, first)
	if metrics.stats.Misses != 1 {
		t.Fatalf("misses = %d, want 1", metrics.stats.Misses)
	}
	first.Response = &interaction.UnifiedResponse{ID: "upstream", Usage: interaction.UnifiedUsage{InputTokens: 10, OutputTokens: 5}}
	if err := WriteBackSemantic(ctx, handler.Exact, first, time.Minute); err != nil {
		t.Fatal(err)
	}
	second := semanticRequest("help me please", false)
	_, _ = handler.Handle(ctx, second)
	if metrics.stats.Hits != 1 {
		t.Fatalf("hits = %d, want 1", metrics.stats.Hits)
	}
}
