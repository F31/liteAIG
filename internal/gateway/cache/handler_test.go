package cache

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/cache"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/pipeline"
	"github.com/F31/liteAIG/internal/kernel/runtime"
)

func testSnapshot(policy runtime.CachePolicy) *runtime.TenantRuntimeSnapshot {
	return runtime.NewTenantSnapshot(runtime.TenantSnapshotData{TenantID: "tenant", TenantRef: "ref", Version: 1, Cache: policy})
}

func chatRequest(temp float64) *interaction.UnifiedRequest {
	return &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "chat", Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "hi"}}, Temperature: &temp}}
}

func TestCacheableRules(t *testing.T) {
	enabled := runtime.CachePolicy{Enabled: true, TTLSeconds: 60, NamespaceVersion: 1, MaxTemperature: 1.0}
	if !Cacheable(chatRequest(0.5), enabled) {
		t.Fatal("low-temp chat should be cacheable")
	}
	if Cacheable(chatRequest(1.5), enabled) {
		t.Fatal("high-temperature chat must not be cached")
	}
	noCache := chatRequest(0.5)
	noCache.Metadata = map[string]string{"cache-control": "no-cache"}
	if Cacheable(noCache, enabled) {
		t.Fatal("no-cache directive must bypass cache")
	}
	tool := chatRequest(0.5)
	tool.Chat.Messages = append(tool.Chat.Messages, interaction.Message{Role: "tool", Content: `{"tool_call":1}`})
	if Cacheable(tool, enabled) {
		t.Fatal("tool calls must not be cached")
	}
	if Cacheable(chatRequest(0.5), runtime.CachePolicy{Enabled: false}) {
		t.Fatal("disabled cache policy must not cache")
	}
	embedding := &interaction.UnifiedRequest{Kind: interaction.RequestEmbedding, Model: "embed", Embedding: &interaction.EmbeddingPayload{Inputs: []string{"x"}}}
	if !Cacheable(embedding, enabled) {
		t.Fatal("embeddings should be cacheable")
	}
	multimodal := chatRequest(0.5)
	multimodal.Chat.Messages[0].Parts = []interaction.ContentPart{{Kind: interaction.ContentImage, MediaType: "image/png", DataURL: "data:image/png;base64,AAAA"}}
	if Cacheable(multimodal, enabled) {
		t.Fatal("image-bearing requests must not be cached")
	}
	responses := &interaction.UnifiedRequest{Kind: interaction.RequestResponses, Model: "chat", Chat: chatRequest(0.5).Chat}
	if Cacheable(responses, enabled) {
		t.Fatal("responses kind must not be cached by default")
	}
}

func TestCacheHitStillRunsOutputGuardrailAndAccounting(t *testing.T) {
	var executionCalls atomic.Int32
	var outputCalls atomic.Int32
	var accountingCalls atomic.Int32
	store := cache.NewMemoryStore(cache.MemoryConfig{Capacity: 16})
	metrics := &recordingMetrics{}

	runner, err := pipeline.NewRunner(pipeline.Handlers{
		Admission: pipeline.HandlerFunc(func(context.Context, *kernel.RequestContext) (pipeline.Directive, error) {
			return pipeline.Continue, nil
		}),
		InputGuardrail: pipeline.HandlerFunc(func(context.Context, *kernel.RequestContext) (pipeline.Directive, error) {
			return pipeline.Continue, nil
		}),
		PolicyCostPreflight: Handler{Store: store, Metrics: metrics},
		Resolution: pipeline.HandlerFunc(func(context.Context, *kernel.RequestContext) (pipeline.Directive, error) {
			return pipeline.Continue, nil
		}),
		ExecutionResilience: pipeline.HandlerFunc(func(ctx context.Context, request *kernel.RequestContext) (pipeline.Directive, error) {
			executionCalls.Add(1)
			request.Response = &interaction.UnifiedResponse{ID: "upstream-1", Model: "chat", Choices: []interaction.Choice{{Message: interaction.Message{Role: "assistant", Content: "hello from provider"}}}}
			return pipeline.Continue, nil
		}),
		OutputStreamGuardrail: pipeline.HandlerFunc(func(context.Context, *kernel.RequestContext) (pipeline.Directive, error) {
			outputCalls.Add(1)
			return pipeline.Continue, nil
		}),
		AccountingAndTelemetry: pipeline.HandlerFunc(func(context.Context, *kernel.RequestContext) (pipeline.Directive, error) {
			accountingCalls.Add(1)
			return pipeline.Continue, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	// First request: miss → execution runs → response written back.
	first := &kernel.RequestContext{RequestID: "r1", Snapshot: testSnapshot(runtime.CachePolicy{Enabled: true, TTLSeconds: 60, NamespaceVersion: 1}), Request: chatRequest(0.3), Interaction: &interaction.Context{TenantID: "tenant", ProjectID: "project"}}
	if err := runner.Run(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if first.CacheHit || !first.CachePending {
		t.Fatalf("first request cache flags = hit:%t pending:%t", first.CacheHit, first.CachePending)
	}
	if err := WriteBack(context.Background(), store, first, time.Minute); err != nil {
		t.Fatal(err)
	}
	if executionCalls.Load() != 1 {
		t.Fatalf("execution calls = %d", executionCalls.Load())
	}

	// Second identical request: cache hit → execution skipped, output guardrail
	// and accounting still run on the cached response.
	second := &kernel.RequestContext{RequestID: "r2", Snapshot: testSnapshot(runtime.CachePolicy{Enabled: true, TTLSeconds: 60, NamespaceVersion: 1}), Request: chatRequest(0.3), Interaction: &interaction.Context{TenantID: "tenant", ProjectID: "project"}}
	if err := runner.Run(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if !second.CacheHit {
		t.Fatal("second request was not a cache hit")
	}
	if second.Response == nil || second.Response.ID != "upstream-1" {
		t.Fatalf("cached response not populated: %+v", second.Response)
	}
	if executionCalls.Load() != 1 {
		t.Fatalf("execution ran on cache hit: calls=%d", executionCalls.Load())
	}
	if outputCalls.Load() != 2 || accountingCalls.Load() != 2 {
		t.Fatalf("output=%d accounting=%d, want 2/2", outputCalls.Load(), accountingCalls.Load())
	}
	if second.Source != "cache" || metrics.stats.Hits != 1 {
		t.Fatalf("source=%q metrics=%+v", second.Source, metrics.stats)
	}
}

type recordingMetrics struct{ stats cache.Stats }

func (m *recordingMetrics) Record(value cache.Stats) {
	m.stats.Hits += value.Hits
	m.stats.Misses += value.Misses
	m.stats.Puts += value.Puts
	m.stats.Evictions += value.Evictions
	m.stats.SavingsTokens += value.SavingsTokens
}

func TestCacheKeyDiffersAcrossProjects(t *testing.T) {
	store := cache.NewMemoryStore(cache.MemoryConfig{Capacity: 16})
	handler := Handler{Store: store}
	policy := runtime.CachePolicy{Enabled: true, TTLSeconds: 60, NamespaceVersion: 1}
	reqA := &kernel.RequestContext{Snapshot: testSnapshot(policy), Request: chatRequest(0.3), Interaction: &interaction.Context{TenantID: "tenant", ProjectID: "project-a"}}
	reqB := &kernel.RequestContext{Snapshot: testSnapshot(policy), Request: chatRequest(0.3), Interaction: &interaction.Context{TenantID: "tenant", ProjectID: "project-b"}}
	if _, err := handler.Handle(context.Background(), reqA); err != nil {
		t.Fatal(err)
	}
	if _, err := handler.Handle(context.Background(), reqB); err != nil {
		t.Fatal(err)
	}
	if reqA.CacheKey == reqB.CacheKey {
		t.Fatal("cross-project requests share a cache key")
	}
}
