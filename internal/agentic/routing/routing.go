// Package routing provides capability-based agent endpoint routing with
// evidence-windowed, explainable scoring. The same deterministic Plan is used
// by production and the explainer.
package routing

import (
	"errors"
	"sort"
	"time"

	"github.com/F31/liteAIG/internal/kernel/runtime"
)

// EndpointMetrics are scoring inputs for one agent endpoint.
type EndpointMetrics struct {
	Health    float64 // 0..1 healthy ratio (higher is better)
	Cost      float64 // lower is better
	LatencyMS float64 // lower is better
}

// AgentScore is an evidence-windowed, explainable endpoint score.
type AgentScore struct {
	EndpointID string
	Health     float64
	Cost       float64
	LatencyMS  float64
	Total      float64
	From       time.Time
	To         time.Time
}

// AgentPlan is the capability-based routing decision.
type AgentPlan struct {
	Capability string
	Selected   string
	Fallback   []string
	Scores     []AgentScore
}

// Router resolves agent endpoints by capability and orders by score.
type Router struct {
	weights map[string]float64 // health|cost|latency
}

// NewRouter builds a capability router with explicit score weights.
func NewRouter(weights map[string]float64) *Router {
	if weights == nil {
		weights = map[string]float64{"health": 1, "cost": 1, "latency": 1}
	}
	return &Router{weights: weights}
}

var ErrNoEligibleAgent = errors.New("no eligible agent endpoint for capability")

// Plan filters endpoints by capability and orders eligible ones by score.
func (r *Router) Plan(snapshot *runtime.TenantRuntimeSnapshot, capability string, metrics map[string]EndpointMetrics, window [2]time.Time) (*AgentPlan, error) {
	if snapshot == nil {
		return nil, ErrNoEligibleAgent
	}
	agentIDs := snapshot.AgentsByCapability(capability)
	eligible := []runtime.AgentEndpoint{}
	for _, id := range agentIDs {
		// Find an endpoint exposing the capability for the agent.
		for _, endpoint := range snapshot.AgentEndpoints() {
			if endpoint.AgentID == id && hasCapability(endpoint.Capabilities, capability) {
				eligible = append(eligible, endpoint)
			}
		}
	}
	if len(eligible) == 0 {
		return nil, ErrNoEligibleAgent
	}
	scores := make([]AgentScore, 0, len(eligible))
	for _, endpoint := range eligible {
		metric := metrics[endpoint.ID]
		score := r.score(endpoint.ID, metric)
		scores = append(scores, score)
	}
	sort.SliceStable(scores, func(i, j int) bool { return scores[i].Total < scores[j].Total })
	plan := &AgentPlan{Capability: capability, Scores: scores}
	plan.Selected = scores[0].EndpointID
	for _, score := range scores {
		plan.Fallback = append(plan.Fallback, score.EndpointID)
	}
	_ = window
	return plan, nil
}

func (r *Router) score(endpointID string, metric EndpointMetrics) AgentScore {
	health := metric.Health
	cost := normLow(metric.Cost)
	latency := normLow(metric.LatencyMS)
	total := r.weights["health"]*(1-health) + r.weights["cost"]*cost + r.weights["latency"]*latency
	return AgentScore{EndpointID: endpointID, Health: health, Cost: metric.Cost, LatencyMS: metric.LatencyMS, Total: total}
}

// normLow bounds a lower-is-better metric to [0,1] with a noise floor.
func normLow(value float64) float64 {
	if value < 0 {
		value = 0
	}
	const floor = 1.0
	scale := value + floor
	normalized := value / scale
	if normalized > 1 {
		return 1
	}
	return normalized
}

func hasCapability(capabilities []string, want string) bool {
	for _, capability := range capabilities {
		if capability == want {
			return true
		}
	}
	return false
}
