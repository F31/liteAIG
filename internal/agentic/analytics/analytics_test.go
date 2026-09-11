package analytics

import (
	"testing"

	"github.com/F31/liteAIG/internal/finops/accounting"
	"github.com/F31/liteAIG/internal/observability/agentgraph"
)

func TestByAgentAggregatesCostAndCalls(t *testing.T) {
	builder := agentgraph.NewBuilder("tenant")
	cost := 5.0
	graph := builder.Build([]accounting.RequestRecord{
		{TenantID: "tenant", RootTaskID: "root", TaskID: "root", AgentID: "agent-a", LogicalModel: "chat", ProviderCost: &cost},
		{TenantID: "tenant", RootTaskID: "root", TaskID: "hop-1", AgentID: "agent-b", LogicalModel: "tool", ProviderCost: &cost},
		{TenantID: "tenant", RootTaskID: "root", TaskID: "hop-2", AgentID: "agent-a", LogicalModel: "chat", ProviderCost: &cost},
	})
	stats := ByAgent(graph)
	if len(stats) != 2 {
		t.Fatalf("stats = %+v", stats)
	}
	for _, agent := range stats {
		switch agent.AgentID {
		case "agent-a":
			if agent.Calls != 2 || agent.Cost != 10 {
				t.Fatalf("agent-a stats = %+v", agent)
			}
		case "agent-b":
			if agent.Calls != 1 || agent.Cost != 5 {
				t.Fatalf("agent-b stats = %+v", agent)
			}
		}
	}
}
