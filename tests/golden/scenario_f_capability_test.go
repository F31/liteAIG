package golden

import (
	"context"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/agentic/recommend"
	"github.com/F31/liteAIG/internal/agentic/routing"
	"github.com/F31/liteAIG/internal/kernel/runtime"
)

func agentOptimizationSnapshot() *runtime.TenantRuntimeSnapshot {
	return runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "org-a", TenantRef: "ref", Version: 6,
		Agents: []runtime.Agent{
			{ID: "agent-a", ProjectID: "project", Name: "A", Status: "active", Capabilities: []string{"invoice.read"}},
			{ID: "agent-b", ProjectID: "project", Name: "B", Status: "active", Capabilities: []string{"invoice.read"}},
		},
		AgentEndpoints: []runtime.AgentEndpoint{
			{ID: "ep-a", AgentID: "agent-a", Capabilities: []string{"invoice.read"}},
			{ID: "ep-b", AgentID: "agent-b", Capabilities: []string{"invoice.read"}},
		},
	})
}

func TestScenarioFCapabilityDrivenAgentSelection(t *testing.T) {
	router := routing.NewRouter(map[string]float64{"cost": 1, "health": 0, "latency": 0})
	window := [2]time.Time{time.Unix(1, 0), time.Unix(2, 0)}
	plan, err := router.Plan(agentOptimizationSnapshot(), "invoice.read", map[string]routing.EndpointMetrics{"ep-a": {Cost: 1}, "ep-b": {Cost: 5}}, window)
	if err != nil || plan.Selected != "ep-a" {
		t.Fatalf("capability plan = %+v, %v", plan, err)
	}
	// Suggest-only: accepting a cheaper-endpoint recommendation creates a draft.
	service := recommend.NewService()
	rec := service.CheaperEndpoint("org-a", "invoice.read", "ep-b", 5, 1, window[0], window[1])
	if rec == nil {
		t.Fatal("expected a cheaper-endpoint recommendation")
	}
	creator := &goldenDraftCreator{}
	draftID, err := rec.Accept(context.Background(), creator, "admin")
	if err != nil || draftID == "" || creator.calls != 1 {
		t.Fatalf("accept = %q, calls=%d, %v", draftID, creator.calls, err)
	}
	if creator.mutated {
		t.Fatal("recommendation mutated production")
	}
}

type goldenDraftCreator struct {
	calls   int
	mutated bool
}

func (d *goldenDraftCreator) CreateDraftForRecommendation(_ context.Context, tenantID, actor string, change map[string]any) (string, error) {
	d.calls++
	return "draft-cap-1", nil
}
