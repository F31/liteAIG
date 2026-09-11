// Package analytics derives read-only per-agent statistics from the Agent/Task
// Graph. No anomaly is auto-applied.
package analytics

import "github.com/F31/liteAIG/internal/observability/agentgraph"

// AgentStats is one agent's cost/latency/loop statistics.
type AgentStats struct {
	AgentID string
	Calls   int
	Cost    float64
	Loops   int
}

// ByAgent aggregates per-agent statistics from a graph.
func ByAgent(graph *agentgraph.Graph) []AgentStats {
	if graph == nil {
		return nil
	}
	stats := map[string]*AgentStats{}
	var order []string
	for _, hop := range graph.Hops {
		if hop.AgentID == "" {
			continue
		}
		agent, ok := stats[hop.AgentID]
		if !ok {
			agent = &AgentStats{AgentID: hop.AgentID}
			stats[hop.AgentID] = agent
			order = append(order, hop.AgentID)
		}
		agent.Calls++
		agent.Cost += hop.Cost
	}
	result := make([]AgentStats, 0, len(order))
	for _, id := range order {
		result = append(result, *stats[id])
	}
	return result
}
