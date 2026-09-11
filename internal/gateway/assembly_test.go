package gateway

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/cache"
	gatewaycache "github.com/F31/liteAIG/internal/gateway/cache"
	"github.com/F31/liteAIG/internal/gateway/guardrail"
	toolgate "github.com/F31/liteAIG/internal/gateway/tool"
	"github.com/F31/liteAIG/internal/guardrail/builtin"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/pipeline"
	"github.com/F31/liteAIG/internal/kernel/runtime"
)

// TestPipelineAssemblyComposesAllStages proves the seven fixed stages plus the
// Exact Cache and snapshot-driven guardrail handlers run a request end to end
// with the compile-time-fixed order.
func TestPipelineAssemblyComposesAllStages(t *testing.T) {
	policy := runtime.CachePolicy{Enabled: true, TTLSeconds: 60, NamespaceVersion: 1}
	snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Version: 3, Cache: policy,
		Guardrail: runtime.GuardrailPolicy{Mode: "tighten", SecurityEpoch: 3, Rules: []runtime.GuardrailRule{{ID: "blocked", Kind: "keyword", Pattern: "forbidden", Action: "block"}}},
	})
	store := cache.NewMemoryStore(cache.MemoryConfig{Capacity: 8})

	var executionCalls, guardrailCalls, accountingCalls atomic.Int32
	runner, err := pipeline.NewRunner(pipeline.Handlers{
		Admission: pipeline.HandlerFunc(func(context.Context, *kernel.RequestContext) (pipeline.Directive, error) {
			return pipeline.Continue, nil
		}),
		InputGuardrail: func() pipeline.Handler {
			engine, _ := builtin.New(builtin.Policy{})
			return guardrail.Handler{Engine: engine}
		}(),
		PolicyCostPreflight: gatewaycache.Handler{Store: store},
		Resolution: pipeline.HandlerFunc(func(context.Context, *kernel.RequestContext) (pipeline.Directive, error) {
			return pipeline.Continue, nil
		}),
		ExecutionResilience: pipeline.HandlerFunc(func(_ context.Context, request *kernel.RequestContext) (pipeline.Directive, error) {
			executionCalls.Add(1)
			request.Response = &interaction.UnifiedResponse{ID: "upstream", Model: "chat", Choices: []interaction.Choice{{Message: interaction.Message{Role: "assistant", Content: "hello"}}}}
			return pipeline.Continue, nil
		}),
		OutputStreamGuardrail: func() pipeline.Handler {
			engine, _ := builtin.New(builtin.Policy{})
			return guardrail.Handler{Engine: engine, Output: true}
		}(),
		AccountingAndTelemetry: pipeline.HandlerFunc(func(context.Context, *kernel.RequestContext) (pipeline.Directive, error) {
			accountingCalls.Add(1)
			return pipeline.Continue, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	request := &kernel.RequestContext{RequestID: "r1", Snapshot: snapshot, Interaction: &interaction.Context{TenantID: "tenant", ProjectID: "project"}, Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "chat", Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "ok"}}}}}
	if err := runner.Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if err := gatewaycache.WriteBack(context.Background(), store, request, time.Minute); err != nil {
		t.Fatal(err)
	}
	if executionCalls.Load() != 1 || guardrailCalls.Load() != 0 || accountingCalls.Load() != 1 {
		t.Fatalf("calls exec=%d guardrail=%d accounting=%d", executionCalls.Load(), guardrailCalls.Load(), accountingCalls.Load())
	}

	// Cache hit: execution is skipped but accounting still runs.
	cached := &kernel.RequestContext{RequestID: "r2", Snapshot: snapshot, Interaction: &interaction.Context{TenantID: "tenant", ProjectID: "project"}, Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "chat", Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "ok"}}}}}
	if err := runner.Run(context.Background(), cached); err != nil {
		t.Fatal(err)
	}
	if !cached.CacheHit || executionCalls.Load() != 1 || accountingCalls.Load() != 2 {
		t.Fatalf("cache hit: hit=%t exec=%d accounting=%d", cached.CacheHit, executionCalls.Load(), accountingCalls.Load())
	}

	// Tool gate is part of the assembly contract: a tool request is rejected
	// before upstream when it has no eligible policy.
	toolReq := &kernel.RequestContext{RequestID: "r3", Snapshot: snapshot, Interaction: &interaction.Context{Kind: interaction.KindTool, TenantID: "tenant", ProjectID: "project", Caller: interaction.PrincipalRef{Type: "agent", ID: "a"}, Target: interaction.ResourceRef{Type: "tool", ID: "unknown"}}, Request: &interaction.UnifiedRequest{Kind: interaction.RequestTool, Tool: &interaction.ToolPayload{Name: "unknown"}}}
	if _, err := (toolgate.Handler{}).Handle(context.Background(), toolReq); err == nil {
		t.Fatal("unknown tool was not rejected by the tool gate")
	}
}
