package golden

import (
	"testing"

	"github.com/F31/liteAIG/internal/finops/accounting"
	"github.com/F31/liteAIG/internal/observability/agentgraph"
)

func TestScenarioFAgentGraphReconstruction(t *testing.T) {
	cost := 5.0
	records := []accounting.RequestRecord{
		{TenantID: "org-a", RootTaskID: "task-1", TaskID: "task-1", AgentID: "agent-a", LogicalModel: "chat", Outcome: "success", ProviderCost: &cost},
		{TenantID: "org-a", RootTaskID: "task-1", TaskID: "hop-1", AgentID: "agent-b", LogicalModel: "invoice.read", Outcome: "success", ProviderCost: &cost},
		{TenantID: "org-a", RootTaskID: "task-1", TaskID: "hop-2", AgentID: "partner-b", LogicalModel: "chat", Outcome: "success", ProviderCost: &cost},
	}
	graph := agentgraph.NewBuilder("org-a").Build(records)
	if len(graph.Hops) != 3 || graph.TotalCost() != 15 {
		t.Fatalf("graph hops=%d total=%f, want 3/15", len(graph.Hops), graph.TotalCost())
	}
	// The chain reconstructs agent → tool/model → next hop in order.
	if graph.Hops[0].AgentID != "agent-a" || graph.Hops[1].Model != "invoice.read" || graph.Hops[2].AgentID != "partner-b" {
		t.Fatalf("chain = %+v", graph.Hops)
	}
	// A trust-boundary crossing on the external hop is attributable.
	graph.Hops[2].CrossesBoundary = true
	if agents := graph.CrossBoundaryAgents(); len(agents) != 1 || agents[0] != "partner-b" {
		t.Fatalf("cross-boundary agents = %v", agents)
	}
}
