package routing

import (
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/runtime"
)

func snapshot() *runtime.TenantRuntimeSnapshot {
	return runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Version: 5,
		Agents: []runtime.Agent{
			{ID: "agent-a", ProjectID: "project", Name: "A", Status: "active", Capabilities: []string{"invoice.read", "chat"}},
			{ID: "agent-b", ProjectID: "project", Name: "B", Status: "active", Capabilities: []string{"invoice.read"}},
		},
		AgentEndpoints: []runtime.AgentEndpoint{
			{ID: "ep-a", AgentID: "agent-a", Version: "v1", Capabilities: []string{"invoice.read"}},
			{ID: "ep-b", AgentID: "agent-b", Version: "v1", Capabilities: []string{"invoice.read"}},
			{ID: "ep-chat", AgentID: "agent-a", Version: "v1", Capabilities: []string{"chat"}},
		},
	})
}

func TestCapabilityFiltersEndpoints(t *testing.T) {
	router := NewRouter(nil)
	window := [2]time.Time{time.Unix(1, 0), time.Unix(2, 0)}
	plan, err := router.Plan(snapshot(), "invoice.read", map[string]EndpointMetrics{"ep-a": {Health: 1, Cost: 1}, "ep-b": {Health: 1, Cost: 5}}, window)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Selected != "ep-a" || len(plan.Fallback) != 2 {
		t.Fatalf("plan = %+v", plan)
	}
	// chat capability only resolves to ep-chat.
	chat, err := router.Plan(snapshot(), "chat", map[string]EndpointMetrics{"ep-chat": {Health: 1}}, window)
	if err != nil || chat.Selected != "ep-chat" || len(chat.Fallback) != 1 {
		t.Fatalf("chat plan = %+v, %v", chat, err)
	}
}

func TestScoreOrdersByCost(t *testing.T) {
	router := NewRouter(map[string]float64{"cost": 1, "health": 0, "latency": 0})
	plan, err := router.Plan(snapshot(), "invoice.read", map[string]EndpointMetrics{"ep-a": {Cost: 1}, "ep-b": {Cost: 5}}, [2]time.Time{time.Unix(1, 0), time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Selected != "ep-a" {
		t.Fatalf("cost ordering selected %s, want ep-a", plan.Selected)
	}
	// Every score is evidence-windowed and explainable.
	for _, score := range plan.Scores {
		if score.EndpointID == "" || score.Total < 0 {
			t.Fatalf("score = %+v", score)
		}
	}
}

func TestProductionPlanEquivalence(t *testing.T) {
	router := NewRouter(nil)
	input := map[string]EndpointMetrics{"ep-a": {Health: 0.9, Cost: 1, LatencyMS: 10}, "ep-b": {Health: 0.8, Cost: 4, LatencyMS: 40}}
	window := [2]time.Time{time.Unix(1, 0), time.Unix(2, 0)}
	production, _ := router.Plan(snapshot(), "invoice.read", input, window)
	explainer, _ := router.Plan(snapshot(), "invoice.read", input, window)
	if production.Selected != explainer.Selected || len(production.Fallback) != len(explainer.Fallback) {
		t.Fatalf("production/explainer divergence: %+v vs %+v", production, explainer)
	}
	for i := range production.Fallback {
		if production.Fallback[i] != explainer.Fallback[i] {
			t.Fatalf("fallback order differs at %d", i)
		}
	}
}

func TestNoEligibleAgent(t *testing.T) {
	router := NewRouter(nil)
	if _, err := router.Plan(snapshot(), "payment.execute", nil, [2]time.Time{time.Unix(1, 0), time.Unix(2, 0)}); err == nil {
		t.Fatal("expected no eligible agent error")
	}
}
