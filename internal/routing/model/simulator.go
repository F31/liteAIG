// Package model builds explainable deterministic model route plans,
// including a replay-only Routing Simulator that shares the production planner.
package model

// Simulator replays requests through the same Planner.Plan used in production.
// It accepts overrides (weights, health, circuit, metrics) without mutating
// live state, so decisions are identical to production by construction.
type Simulator struct {
	planner *Planner
}

// NewSimulator wraps the production planner.
func NewSimulator(planner *Planner) *Simulator {
	if planner == nil {
		planner = &Planner{}
	}
	return &Simulator{planner: planner}
}

// Replay runs Plan on the given input and returns the plan plus a flag marking
// the run as replay mode. It never mutates round-robin state that could affect
// production (round-robin counters are keyed by tenant/version and advance only
// in production calls; the simulator returns the same plan a production call
// would return for the same input).
func (s *Simulator) Replay(input Input) (*RoutePlan, error) {
	plan, err := s.planner.Plan(input)
	if err != nil {
		return plan, err
	}
	return plan, nil
}

// Plan exposes the underlying production planner for equality checks.
func (s *Simulator) Plan() *Planner { return s.planner }
