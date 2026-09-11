package agentgraph

import (
	"testing"

	"github.com/F31/liteAIG/internal/finops/accounting"
)

func request(tenant, root, task, agent, model, deployment, outcome string, cost float64) accounting.RequestRecord {
	return accounting.RequestRecord{
		TenantID: tenant, RootTaskID: root, TaskID: task, AgentID: agent,
		LogicalModel: model, DeploymentID: deployment, Outcome: outcome,
		ProviderCost: &cost,
	}
}

func TestMultiHopChainReconstruction(t *testing.T) {
	builder := NewBuilder("tenant")
	graph := builder.Build([]accounting.RequestRecord{
		request("tenant", "root-1", "root-1", "agent-a", "chat", "d1", "success", 2),
		request("tenant", "root-1", "hop-1", "agent-b", "tool", "d2", "success", 3),
		request("tenant", "root-1", "hop-2", "agent-b", "chat", "d3", "success", 5),
	})
	if graph.RootTask != "root-1" {
		t.Fatalf("root = %s", graph.RootTask)
	}
	chain := graph.RootChain()
	if len(chain) != 3 {
		t.Fatalf("chain length = %d, want 3", len(chain))
	}
	// Root total equals per-hop sum.
	if graph.TotalCost() != 10 {
		t.Fatalf("total = %f, want 10", graph.TotalCost())
	}
	perHop := graph.PerHopCost()
	sum := 0.0
	for _, cost := range perHop {
		sum += cost
	}
	if sum != graph.TotalCost() {
		t.Fatalf("per-hop sum %f != total %f", sum, graph.TotalCost())
	}
}

func TestDeterministicOrderByTaskLinkage(t *testing.T) {
	builder := NewBuilder("tenant")
	graph := builder.Build([]accounting.RequestRecord{
		request("tenant", "root-2", "hop-2", "agent-b", "chat", "d3", "success", 5),
		request("tenant", "root-2", "hop-1", "agent-a", "chat", "d1", "success", 2),
		request("tenant", "root-2", "root-2", "agent-a", "chat", "d1", "success", 2),
	})
	orders := make([]int, 0, len(graph.Hops))
	for _, hop := range graph.Hops {
		orders = append(orders, hop.Order)
	}
	// Orders are 1,2,3 deterministically, regardless of input order.
	expected := []int{1, 2, 3}
	for i, order := range orders {
		if order != expected[i] {
			t.Fatalf("order = %v, want %v", orders, expected)
		}
	}
}

func TestTenantIsolation(t *testing.T) {
	builder := NewBuilder("tenant-a")
	graph := builder.Build([]accounting.RequestRecord{
		request("tenant-a", "root-a", "root-a", "agent-a", "chat", "d1", "success", 2),
		request("tenant-b", "root-b", "root-b", "agent-b", "chat", "d9", "success", 9),
	})
	if graph.RootTask != "root-a" {
		t.Fatalf("cross-tenant data leaked: root = %s", graph.RootTask)
	}
	if graph.TotalCost() != 2 {
		t.Fatalf("cross-tenant cost leaked: %f", graph.TotalCost())
	}
}

func TestCrossBoundaryAgentsFlagged(t *testing.T) {
	builder := NewBuilder("tenant")
	graph := builder.Build([]accounting.RequestRecord{
		request("tenant", "root-3", "root-3", "agent-a", "chat", "d1", "success", 2),
	})
	// Mark the second hop as crossing a trust boundary.
	graph.Hops[0].CrossesBoundary = true
	graph.Hops[0].AgentID = "partner-b"
	if agents := graph.CrossBoundaryAgents(); len(agents) != 1 || agents[0] != "partner-b" {
		t.Fatalf("cross-boundary agents = %v", agents)
	}
}

func TestMalformedLinkageTolerated(t *testing.T) {
	builder := NewBuilder("tenant")
	graph := builder.Build([]accounting.RequestRecord{
		request("tenant", "", "", "agent-a", "chat", "d1", "success", 2), // no root/task
		request("tenant", "root-4", "root-4", "agent-a", "chat", "d1", "success", 2),
	})
	if graph.RootTask != "root-4" {
		t.Fatalf("orphan hop polluted the graph: root = %q", graph.RootTask)
	}
	if graph.TotalCost() != 2 {
		t.Fatalf("orphan cost counted: %f", graph.TotalCost())
	}
}

func TestSpanAttributeReconstructionParity(t *testing.T) {
	facts := []accounting.RequestRecord{
		request("tenant", "root-5", "root-5", "agent-a", "chat", "d1", "success", 2),
		request("tenant", "root-5", "hop-1", "agent-b", "tool", "d2", "success", 3),
	}
	factsGraph := NewBuilder("tenant").Build(facts)

	spans := []SpanAttributes{
		{TaskID: "root-5", RootTaskID: "root-5", AgentID: "agent-a", Model: "chat"},
		{TaskID: "hop-1", RootTaskID: "root-5", ParentTaskID: "root-5", AgentID: "agent-b", Model: "tool"},
	}
	spanGraph := FromSpanAttributes("tenant", spans)
	if !SameChain(factsGraph, spanGraph) {
		t.Fatalf("span reconstruction differs: facts=%+v spans=%+v", factsGraph.Hops, spanGraph.Hops)
	}
}
