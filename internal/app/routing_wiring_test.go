package app

import (
	"context"
	"reflect"
	"testing"

	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/resilience/circuit"
	routing "github.com/F31/liteAIG/internal/routing/model"
)

// routeTestSnapshot has two deployments with a soft route policy.
func routeTestSnapshot() *runtime.TenantRuntimeSnapshot {
	return runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Status: "active", Version: 3,
		Projects:    []runtime.Project{{ID: "project", Status: "active"}},
		Providers:   []runtime.Provider{{ID: "provider", Status: "enabled"}},
		Credentials: []runtime.Credential{{ID: "credential", ProviderID: "provider", Status: "enabled"}},
		Deployments: []runtime.Deployment{
			{ID: "cheap", ProviderID: "provider", CredentialID: "credential", Status: "enabled", Capabilities: []string{"chat"}, ContextWindow: 1000},
			{ID: "fast", ProviderID: "provider", CredentialID: "credential", Status: "enabled", Capabilities: []string{"chat"}, ContextWindow: 1000},
		},
		RoutePolicies: []runtime.RoutePolicy{{ID: "route", ProjectID: "project", Strategy: "soft", DeploymentIDs: []string{"cheap", "fast"}, ScoreWeights: map[string]float64{"cost": 1, "latency": 0, "load": 0, "cache": 0}, Version: 2}},
		LogicalModels: []runtime.LogicalModel{{ID: "logical", Alias: "default-chat", RoutePolicyID: "route"}},
	})
}

// TestRouteHealthTracksCircuitState verifies the routing health map marks a
// deployment with an open circuit unhealthy, feeding the planner's unhealthy
// exclusion.
func TestRouteHealthTracksCircuitState(t *testing.T) {
	snapshot := routeTestSnapshot()
	breaker := circuit.New(circuit.DefaultConfig(), systemClock{})
	p := &litePipeline{circuit: breaker, clock: systemClock{}, metrics: newRoutingMetrics(systemClock{})}
	for i := 0; i < circuit.DefaultConfig().MinSamples; i++ {
		breaker.Record("fast", "credential", false)
	}

	health := p.routeHealth(context.Background(), snapshot)
	if !health["cheap"] {
		t.Fatal("cheap must stay healthy")
	}
	if health["fast"] {
		t.Fatal("fast with open circuit must be unhealthy")
	}

	// The planner must exclude the unhealthy deployment.
	plan, err := (&routing.Planner{}).Plan(routing.Input{
		Snapshot: snapshot, LogicalModel: "default-chat", ProjectID: "project",
		RequestID: "r1", RequiredCapabilities: []string{"chat"},
		Health:  health,
		Metrics: map[string]routing.DeploymentMetrics{"cheap": {Cost: 0.01}, "fast": {Cost: 0.0001}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Selected() != "cheap" {
		t.Fatalf("selected %q, want cheap (fast unhealthy)", plan.Selected())
	}
	for _, evidence := range plan.Evidence() {
		if evidence.DeploymentID == "fast" && evidence.Eligible {
			t.Fatal("fast must be excluded as unhealthy")
		}
	}
}

// TestRoutingMetricsFeedBackIntoPlan verifies observations from one request
// change the next routing decision: the soft strategy picks the cheaper
// deployment once real cost data is recorded.
func TestRoutingMetricsFeedBackIntoPlan(t *testing.T) {
	snapshot := routeTestSnapshot()
	p := &litePipeline{metrics: newRoutingMetrics(systemClock{}), clock: systemClock{}}

	// Feed cost observations: cheap is cheaper, fast is pricier.
	p.metrics.observe("cheap", 100, 0.01, 0, true)
	p.metrics.observe("fast", 10, 0.50, 0, true)

	metrics := p.metrics.snapshot(p.clock.Now())
	plan, err := (&routing.Planner{}).Plan(routing.Input{
		Snapshot: snapshot, LogicalModel: "default-chat", ProjectID: "project",
		RequestID: "r1", RequiredCapabilities: []string{"chat"},
		Metrics: metrics,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Selected() != "cheap" {
		t.Fatalf("soft strategy selected %q, want cheap (cost-weighted)", plan.Selected())
	}
	want := []string{"cheap", "fast"}
	if !reflect.DeepEqual(plan.Fallback(), want) {
		t.Fatalf("soft order = %v, want %v", plan.Fallback(), want)
	}
}

// TestSimulatorMatchesProductionWithHealthAndMetrics verifies the routing
// simulator returns the same decision as production when both are fed the same
// live health and metrics inputs (simulator/production parity with Stage 10
// wiring).
func TestSimulatorMatchesProductionWithHealthAndMetrics(t *testing.T) {
	snapshot := routeTestSnapshot()
	planner := &routing.Planner{}
	simulator := routing.NewSimulator(planner)
	input := routing.Input{
		Snapshot: snapshot, LogicalModel: "default-chat", ProjectID: "project",
		RequestID: "r2", RequiredCapabilities: []string{"chat"},
		Health:  map[string]bool{"cheap": true, "fast": false},
		Metrics: map[string]routing.DeploymentMetrics{"cheap": {Cost: 0.01}, "fast": {Cost: 0.0001}},
	}
	production, err := planner.Plan(input)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := simulator.Replay(input)
	if err != nil {
		t.Fatal(err)
	}
	if production.Selected() != replay.Selected() || !reflect.DeepEqual(production.Fallback(), replay.Fallback()) {
		t.Fatalf("simulator divergence: %s vs %s", production.Selected(), replay.Selected())
	}
}

// TestRouteHealthFallsBackToProbe verifies cold deployments (no observations
// yet) consult the upstream provider probe; a failing probe marks the
// deployment unhealthy.
func TestRouteHealthFallsBackToProbe(t *testing.T) {
	snapshot := routeTestSnapshot()
	probe := func(_ context.Context, _, _, _ string) (bool, string) {
		return false, "unreachable"
	}
	p := &litePipeline{clock: systemClock{}, metrics: newRoutingMetrics(systemClock{}), probeFn: probe}
	health := p.routeHealth(context.Background(), snapshot)
	if health["cheap"] || health["fast"] {
		t.Fatalf("cold deployments with a failing probe must be unhealthy: %+v", health)
	}

	// With observations present, the probe is bypassed and the window rules.
	p.metrics.observe("cheap", 10, 0.01, 0, true)
	health = p.routeHealth(context.Background(), snapshot)
	if !health["cheap"] {
		t.Fatal("cheap with recent success must be healthy despite a failing probe")
	}
	if health["fast"] {
		t.Fatal("fast (cold, failing probe) must stay unhealthy")
	}
}
