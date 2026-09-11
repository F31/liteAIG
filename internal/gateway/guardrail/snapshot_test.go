package guardrail

import (
	"context"
	"testing"

	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
)

func TestHandlerEnforcesSnapshotCompiledPolicy(t *testing.T) {
	snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "t", TenantRef: "ref", Version: 5,
		Guardrail: runtime.GuardrailPolicy{Mode: "tighten", SecurityEpoch: 5, Rules: []runtime.GuardrailRule{{ID: "blocked", Kind: "keyword", Pattern: "forbidden", Action: "block"}}},
	})
	// No fixed engine: the handler must build one from the snapshot.
	handler := Handler{}
	request := &kernel.RequestContext{RequestID: "r", Snapshot: snapshot, Interaction: &interaction.Context{TenantID: "t", ProjectID: "p"}, Request: &interaction.UnifiedRequest{Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "this is forbidden"}}}}}
	if _, err := handler.Handle(context.Background(), request); err == nil {
		t.Fatal("snapshot-compiled policy did not block content")
	}
	// The snapshot version is immutable in flight: a second request with a
	// loosened snapshot is governed by its own captured version.
	loosened := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "t", TenantRef: "ref", Version: 6,
		Guardrail: runtime.GuardrailPolicy{Mode: "loosen", SecurityEpoch: 6, Rules: []runtime.GuardrailRule{}},
	})
	request2 := &kernel.RequestContext{RequestID: "r2", Snapshot: loosened, Interaction: &interaction.Context{TenantID: "t", ProjectID: "p"}, Request: &interaction.UnifiedRequest{Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "this is forbidden"}}}}}
	if _, err := handler.Handle(context.Background(), request2); err != nil {
		t.Fatalf("loosened snapshot still blocked: %v", err)
	}
}
