package model

import (
	"reflect"
	"testing"

	"github.com/F31/liteAIG/internal/kernel/runtime"
)

func softSnapshot() *runtime.TenantRuntimeSnapshot {
	return runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Status: "active", Version: 3,
		Projects:    []runtime.Project{{ID: "project", Status: "active", AllowedDataRegions: []string{"us"}, ResidencyEnforcement: "strict"}},
		Providers:   []runtime.Provider{{ID: "provider", Status: "enabled"}},
		Credentials: []runtime.Credential{{ID: "credential", ProviderID: "provider", Status: "enabled"}},
		Deployments: []runtime.Deployment{
			{ID: "cheap", ProviderID: "provider", CredentialID: "credential", Status: "enabled", DataRegion: "us", Capabilities: []string{"chat"}, ContextWindow: 100},
			{ID: "fast", ProviderID: "provider", CredentialID: "credential", Status: "enabled", DataRegion: "us", Capabilities: []string{"chat"}, ContextWindow: 100},
			{ID: "slow", ProviderID: "provider", CredentialID: "credential", Status: "enabled", DataRegion: "us", Capabilities: []string{"chat"}, ContextWindow: 100},
		},
		RoutePolicies: []runtime.RoutePolicy{{ID: "route", ProjectID: "project", Strategy: "soft", DeploymentIDs: []string{"cheap", "fast", "slow"}, ScoreWeights: map[string]float64{"cost": 1, "latency": 0, "load": 0, "cache": 0}, Version: 2}},
		LogicalModels: []runtime.LogicalModel{{ID: "logical", Alias: "default-chat", RoutePolicyID: "route"}},
	})
}

func TestSoftScorePrefersLowerCostAndKeepsEvidence(t *testing.T) {
	planner := &Planner{}
	plan, err := planner.Plan(Input{
		Snapshot: softSnapshot(), LogicalModel: "default-chat", ProjectID: "project",
		RequestID: "r1", RequiredCapabilities: []string{"chat"},
		Metrics: map[string]DeploymentMetrics{
			"cheap": {Cost: 0.01, LatencyMS: 500},
			"fast":  {Cost: 0.10, LatencyMS: 50},
			"slow":  {Cost: 0.05, LatencyMS: 50},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Selected() != "cheap" {
		t.Fatalf("soft score selected %q, want cheap", plan.Selected())
	}
	// Cost-only weights: cheap should rank first, then slow, then fast.
	want := []string{"cheap", "slow", "fast"}
	if !reflect.DeepEqual(plan.Fallback(), want) {
		t.Fatalf("soft order = %v, want %v", plan.Fallback(), want)
	}
	// Evidence carries per-component breakdown.
	for _, evidence := range plan.Evidence() {
		if evidence.Eligible && evidence.ScoreBreakdown == nil {
			t.Fatalf("eligible candidate %s missing score breakdown", evidence.DeploymentID)
		}
	}
}

func TestSoftScoreHardConstraintsExcludeFirst(t *testing.T) {
	snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Status: "active", Version: 3,
		Projects:    []runtime.Project{{ID: "project", Status: "active", AllowedDataRegions: []string{"us"}, ResidencyEnforcement: "strict"}},
		Providers:   []runtime.Provider{{ID: "provider", Status: "enabled"}},
		Credentials: []runtime.Credential{{ID: "credential", ProviderID: "provider", Status: "enabled"}},
		Deployments: []runtime.Deployment{
			{ID: "cheap", ProviderID: "provider", CredentialID: "credential", Status: "enabled", DataRegion: "us", Capabilities: []string{"chat"}, ContextWindow: 100},
			{ID: "fast", ProviderID: "provider", CredentialID: "credential", Status: "enabled", DataRegion: "us", Capabilities: []string{"chat"}, ContextWindow: 100},
			{ID: "slow", ProviderID: "provider", CredentialID: "credential", Status: "enabled", DataRegion: "eu", Capabilities: []string{"chat"}, ContextWindow: 100},
		},
		RoutePolicies: []runtime.RoutePolicy{{ID: "route", ProjectID: "project", Strategy: "soft", DeploymentIDs: []string{"cheap", "fast", "slow"}, ScoreWeights: map[string]float64{"cost": 1}, Version: 2}},
		LogicalModels: []runtime.LogicalModel{{ID: "logical", Alias: "default-chat", RoutePolicyID: "route"}},
	})
	planner := &Planner{}
	plan, err := planner.Plan(Input{
		Snapshot: snapshot, LogicalModel: "default-chat", ProjectID: "project",
		RequestID: "r1", RequiredCapabilities: []string{"chat"},
		Metrics: map[string]DeploymentMetrics{
			"cheap": {Cost: 0.01, LatencyMS: 500},
			"fast":  {Cost: 0.10, LatencyMS: 50},
			"slow":  {Cost: 0.0001, LatencyMS: 50}, // cheapest but wrong region
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Selected() != "cheap" {
		t.Fatalf("selected %q, want cheap (slow excluded by residency)", plan.Selected())
	}
	for _, evidence := range plan.Evidence() {
		if evidence.DeploymentID == "slow" && evidence.Eligible {
			t.Fatal("slow (wrong region) must be excluded before scoring")
		}
	}
}

func TestSoftDefaultsPreservePriorityWhenUnweighted(t *testing.T) {
	snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Status: "active", Version: 3,
		Projects:      []runtime.Project{{ID: "project", Status: "active"}},
		Providers:     []runtime.Provider{{ID: "provider", Status: "enabled"}},
		Credentials:   []runtime.Credential{{ID: "credential", ProviderID: "provider", Status: "enabled"}},
		Deployments:   []runtime.Deployment{{ID: "low", ProviderID: "provider", CredentialID: "credential", Status: "enabled", Priority: 2}, {ID: "high", ProviderID: "provider", CredentialID: "credential", Status: "enabled", Priority: 1}},
		RoutePolicies: []runtime.RoutePolicy{{ID: "route", ProjectID: "project", Strategy: "priority", DeploymentIDs: []string{"low", "high"}, Version: 1}},
		LogicalModels: []runtime.LogicalModel{{ID: "logical", Alias: "default-chat", RoutePolicyID: "route"}},
	})
	plan, err := (&Planner{}).Plan(Input{Snapshot: snapshot, LogicalModel: "default-chat", ProjectID: "project", RequestID: "r"})
	if err != nil || plan.Selected() != "high" {
		t.Fatalf("priority default = %s, %v", plan.Selected(), err)
	}
}

func TestSimulatorMatchesProductionDecision(t *testing.T) {
	planner := &Planner{}
	simulator := NewSimulator(planner)
	input := Input{Snapshot: softSnapshot(), LogicalModel: "default-chat", ProjectID: "project", RequestID: "r2", RequiredCapabilities: []string{"chat"}, Metrics: map[string]DeploymentMetrics{"cheap": {Cost: 0.01}, "fast": {Cost: 0.10}, "slow": {Cost: 0.05}}}
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
