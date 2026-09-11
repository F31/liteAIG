// Package agentgraph reconstructs the Agent/Task call graph from persisted
// request/usage facts (task/root/parent/agent linkage) and exposes per-hop
// queries. There is no second "agentic_spans" fact source: the graph is derived
// from the Usage/Request ledger.
package agentgraph

import (
	"sort"

	"github.com/F31/liteAIG/internal/finops/accounting"
)

// NodeKind is the graph node type.
type NodeKind string

const (
	NodeTask  NodeKind = "task"
	NodeAgent NodeKind = "agent"
	NodeTool  NodeKind = "tool"
	NodeModel NodeKind = "model"
)

// Node is one graph vertex.
type Node struct {
	Kind NodeKind
	ID   string
	Name string
}

// Edge is one directed call/delegation between nodes.
type Edge struct {
	From, To        Node
	RequestID       string
	DeploymentID    string
	Outcome         string
	Cost            float64
	Provenance      string
	CrossesBoundary bool
}

// Graph is a tenant-scoped agent/task call graph.
type Graph struct {
	TenantID string
	RootTask string
	Nodes    []Node
	Edges    []Edge
	Hops     []Hop
}

// Hop is one per-hop cost/breakdown row.
type Hop struct {
	Order           int
	AgentID         string
	ToolID          string
	Model           string
	Cost            float64
	Outcome         string
	CrossesBoundary bool
}

// Builder reconstructs a graph from facts for one tenant.
type Builder struct {
	TenantID string
}

// NewBuilder creates a tenant-scoped graph builder.
func NewBuilder(tenantID string) *Builder { return &Builder{TenantID: tenantID} }

// Build reconstructs the graph for a root task from request records.
func (b *Builder) Build(requests []accounting.RequestRecord) *Graph {
	graph := &Graph{TenantID: b.TenantID}
	seen := map[string]bool{}
	var hops []Hop
	order := 0
	for _, request := range requests {
		if request.TenantID != b.TenantID {
			continue
		}
		root := request.RootTaskID
		if root == "" {
			root = request.TaskID
		}
		if root == "" {
			continue
		}
		if graph.RootTask == "" {
			graph.RootTask = root
		} else if graph.RootTask != root {
			continue
		}
		graph.addNode(NodeTask, root, root)
		graph.addNode(NodeModel, request.LogicalModel, request.LogicalModel)
		if request.AgentID != "" {
			graph.addNode(NodeAgent, request.AgentID, request.AgentID)
		}
		graph.Edges = append(graph.Edges, Edge{
			From:         Node{Kind: NodeTask, ID: root, Name: root},
			To:           Node{Kind: NodeModel, ID: request.LogicalModel, Name: request.LogicalModel},
			RequestID:    request.RequestID,
			DeploymentID: request.DeploymentID,
			Outcome:      request.Outcome,
			Cost:         costValue(request),
			Provenance:   "model_result",
		})
		graph.Edges[len(graph.Edges)-1].CrossesBoundary = false
		hopKey := request.TaskID
		if hopKey == "" {
			hopKey = request.RequestID
		}
		if !seen[hopKey] {
			seen[hopKey] = true
			order++
			hops = append(hops, Hop{Order: order, AgentID: request.AgentID, Model: request.LogicalModel, Cost: costValue(request), Outcome: request.Outcome})
		}
	}
	graph.Hops = hops
	sort.Slice(graph.Hops, func(i, j int) bool { return graph.Hops[i].Order < graph.Hops[j].Order })
	return graph
}

func (g *Graph) addNode(kind NodeKind, id, name string) {
	for _, node := range g.Nodes {
		if node.Kind == kind && node.ID == id {
			return
		}
	}
	g.Nodes = append(g.Nodes, Node{Kind: kind, ID: id, Name: name})
}

// RootChain returns the ordered per-hop chain.
func (g *Graph) RootChain() []Hop { return g.Hops }

// PerHopCost returns each hop's cost.
func (g *Graph) PerHopCost() map[int]float64 {
	result := map[int]float64{}
	for _, hop := range g.Hops {
		result[hop.Order] = hop.Cost
	}
	return result
}

// TotalCost returns the sum of all hops.
func (g *Graph) TotalCost() float64 {
	total := 0.0
	for _, hop := range g.Hops {
		total += hop.Cost
	}
	return total
}

// CrossBoundaryAgents returns agents involved in hops that crossed a trust
// boundary.
func (g *Graph) CrossBoundaryAgents() []string {
	seen := map[string]bool{}
	var result []string
	for _, hop := range g.Hops {
		if hop.CrossesBoundary && hop.AgentID != "" && !seen[hop.AgentID] {
			seen[hop.AgentID] = true
			result = append(result, hop.AgentID)
		}
	}
	return result
}

// SpanAttributes are the task correlation attributes carried by an OTel span.
type SpanAttributes struct {
	TaskID       string
	RootTaskID   string
	ParentTaskID string
	AgentID      string
	Model        string
}

// FromSpanAttributes reconstructs the task chain from span attributes alone,
// producing the same per-hop structure (nodes/edges) as facts-based
// reconstruction. No separate "agentic_spans" source is introduced.
func FromSpanAttributes(tenantID string, spans []SpanAttributes) *Graph {
	graph := &Graph{TenantID: tenantID}
	seen := map[string]bool{}
	var hops []Hop
	order := 0
	for _, span := range spans {
		root := span.RootTaskID
		if root == "" {
			root = span.TaskID
		}
		if root == "" {
			continue
		}
		if graph.RootTask == "" {
			graph.RootTask = root
		} else if graph.RootTask != root {
			continue
		}
		graph.addNode(NodeTask, root, root)
		graph.addNode(NodeModel, span.Model, span.Model)
		if span.AgentID != "" {
			graph.addNode(NodeAgent, span.AgentID, span.AgentID)
		}
		hopKey := span.TaskID
		if !seen[hopKey] {
			seen[hopKey] = true
			order++
			hops = append(hops, Hop{Order: order, AgentID: span.AgentID, Model: span.Model})
		}
	}
	graph.Hops = hops
	sort.Slice(graph.Hops, func(i, j int) bool { return graph.Hops[i].Order < graph.Hops[j].Order })
	return graph
}

// SameChain reports whether two graphs share the same node set and hop order.
func SameChain(a, b *Graph) bool {
	if a == nil || b == nil || a.RootTask != b.RootTask || len(a.Hops) != len(b.Hops) {
		return false
	}
	for i := range a.Hops {
		if a.Hops[i].AgentID != b.Hops[i].AgentID || a.Hops[i].Model != b.Hops[i].Model {
			return false
		}
	}
	return true
}

func costValue(request accounting.RequestRecord) float64 {
	if request.ProviderCost != nil {
		return *request.ProviderCost
	}
	return 0
}
