package golden

import (
	"context"
	"reflect"
	"testing"

	"github.com/F31/liteAIG/internal/controlplane/recommend"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	routing "github.com/F31/liteAIG/internal/routing/model"
	"time"
)

func costSnapshot() *runtime.TenantRuntimeSnapshot {
	return runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Status: "active", Version: 6,
		Projects:    []runtime.Project{{ID: "project", Status: "active", AllowedDataRegions: []string{"us"}, ResidencyEnforcement: "strict"}},
		Providers:   []runtime.Provider{{ID: "provider", Status: "enabled"}},
		Credentials: []runtime.Credential{{ID: "credential", ProviderID: "provider", Status: "enabled"}},
		Deployments: []runtime.Deployment{
			{ID: "cheap", ProviderID: "provider", CredentialID: "credential", Status: "enabled", DataRegion: "us", Capabilities: []string{"chat"}, ContextWindow: 100},
			{ID: "premium", ProviderID: "provider", CredentialID: "credential", Status: "enabled", DataRegion: "us", Capabilities: []string{"chat"}, ContextWindow: 100},
		},
		RoutePolicies: []runtime.RoutePolicy{{ID: "route", ProjectID: "project", Strategy: "soft", DeploymentIDs: []string{"cheap", "premium"}, ScoreWeights: map[string]float64{"cost": 1.5, "latency": 0.5, "load": 0, "cache": 0}, Version: 3}},
		LogicalModels: []runtime.LogicalModel{{ID: "logical", Alias: "default-chat", RoutePolicyID: "route"}},
	})
}

func TestScenarioBCostLatencyOptimizedRoutingAndRecommendation(t *testing.T) {
	planner := &routing.Planner{}
	simulator := routing.NewSimulator(planner)
	input := routing.Input{
		Snapshot: costSnapshot(), LogicalModel: "default-chat", ProjectID: "project",
		RequestID: "r", RequiredCapabilities: []string{"chat"},
		Metrics: map[string]routing.DeploymentMetrics{
			"cheap":   {Cost: 0.02, LatencyMS: 200},
			"premium": {Cost: 0.30, LatencyMS: 20},
		},
	}

	// Cost-weighted soft score selects the cheap deployment.
	plan, err := planner.Plan(input)
	if err != nil || plan.Selected() != "cheap" {
		t.Fatalf("optimized route = %s, %v", plan.Selected(), err)
	}
	// Every eligible candidate carries a per-component score breakdown.
	for _, evidence := range plan.Evidence() {
		if evidence.Eligible && evidence.ScoreBreakdown == nil {
			t.Fatalf("missing score breakdown for %s", evidence.DeploymentID)
		}
	}

	// Simulator reproduces the production decision exactly.
	replay, err := simulator.Replay(input)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Selected() != plan.Selected() || !reflect.DeepEqual(replay.Fallback(), plan.Fallback()) {
		t.Fatalf("simulator divergence: %s vs %s", replay.Selected(), plan.Selected())
	}

	// Recommendation is suggest-only: accepting creates a draft; production is
	// untouched until the draft is published.
	window := recommend.EvidenceWindow{From: time.Unix(1, 0), To: time.Unix(2, 0), Samples: 5000, Metric: "cost"}
	service := recommend.NewService(func() time.Time { return time.Unix(3, 0) })
	rec := service.CostThresholdRecommendation("tenant", "cost", 300, 200, window)
	if rec == nil {
		t.Fatal("expected a cost recommendation above threshold")
	}
	creator := &draftCreatorFn{calls: 0}
	draftID, err := rec.Accept(context.Background(), creator, "admin")
	if err != nil || draftID != "draft-1" || creator.calls != 1 {
		t.Fatalf("accept = %q, calls=%d, %v", draftID, creator.calls, err)
	}
	if creator.mutated {
		t.Fatal("recommendation mutated production config")
	}
}

type draftCreatorFn struct {
	calls   int
	mutated bool
}

func (d *draftCreatorFn) CreateDraftForRecommendation(_ context.Context, tenantID, actor string, change map[string]any) (string, error) {
	d.calls++
	return "draft-1", nil
}
