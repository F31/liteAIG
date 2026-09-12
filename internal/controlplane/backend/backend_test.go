package backend

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/finops/accounting"
	"github.com/F31/liteAIG/internal/finops/pricing"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/observability/alert"
	"github.com/F31/liteAIG/internal/tenancy"
)

type recordingCircuits struct{ reset []string }

func (c *recordingCircuits) State(string, string) string { return "open" }
func (c *recordingCircuits) Reset(deploymentID, credentialID string) {
	c.reset = append(c.reset, deploymentID+":"+credentialID)
}

type finopsAccounting struct{ requests []accounting.RequestRecord }

func (r finopsAccounting) Finalize(context.Context, accounting.Facts) (bool, error) {
	return false, nil
}
func (r finopsAccounting) GetRequest(context.Context, tenancy.TenantScope, string) (*accounting.RequestRecord, error) {
	return nil, nil
}
func (r finopsAccounting) ListRequests(context.Context, tenancy.TenantScope, int) ([]accounting.RequestRecord, error) {
	return r.requests, nil
}
func (r finopsAccounting) GetUsage(context.Context, tenancy.TenantScope, string) (*accounting.UsageRecord, error) {
	return nil, nil
}

func TestControlBackendHealthAndReset(t *testing.T) {
	registry := &runtime.ActiveRegistry{}
	registry.ActivateTenant("ref", runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Status: "active",
		Providers:   []runtime.Provider{{ID: "p1", Type: "openai", Status: "enabled"}},
		Credentials: []runtime.Credential{{ID: "c1", ProviderID: "p1", Status: "enabled"}},
		Deployments: []runtime.Deployment{{ID: "d1", ProviderID: "p1", CredentialID: "c1", Status: "enabled"}},
	}))
	circuits := &recordingCircuits{}
	backend := NewControlBackend(nil, nil, nil, nil, nil, registry, circuits, nil, nil, nil)

	health, err := backend.Health(context.Background(), tenancy.TenantScope{TenantID: "tenant"})
	if err != nil {
		t.Fatal(err)
	}
	if !health.Ready || len(health.Providers) != 1 || len(health.Circuits) != 1 {
		t.Fatalf("health = %+v", health)
	}
	if err := backend.ResetCircuit(context.Background(), tenancy.TenantScope{TenantID: "tenant"}, "d1", "c1", "admin"); err != nil {
		t.Fatal(err)
	}
	if len(circuits.reset) != 1 || circuits.reset[0] != "d1:c1" {
		t.Fatalf("resets = %v", circuits.reset)
	}
}

func TestRuntimeResourcesExposeBudgetConsistency(t *testing.T) {
	registry := &runtime.ActiveRegistry{}
	registry.ActivateTenant("ref", runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Status: "active",
		BudgetPolicies: []runtime.BudgetPolicy{{
			ID: "budget-1", ProjectID: "project", WindowHours: 24,
			TokenLimit: 1000, Mode: "hard", Consistency: "global_soft",
		}},
	}))
	backend := NewControlBackend(nil, nil, nil, nil, nil, registry, nil, nil, nil, nil)
	resources, err := backend.Runtime(context.Background(), tenancy.TenantScope{TenantID: "tenant"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resources.Budgets) != 1 || resources.Budgets[0].Consistency != "global_soft" {
		t.Fatalf("budgets = %+v", resources.Budgets)
	}
}

func TestControlBackendFinOpsAggregatesRecentRequests(t *testing.T) {
	costA := 0.30
	costB := 0.10
	registry := &runtime.ActiveRegistry{}
	registry.ActivateTenant("tenant-ref", runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "tenant-ref", Status: "active", Version: 1,
		Deployments:   []runtime.Deployment{{ID: "deployment", Status: "enabled", UpstreamModel: "gpt-4o-mini"}},
		RoutePolicies: []runtime.RoutePolicy{{ID: "route", DeploymentIDs: []string{"deployment"}}},
		LogicalModels: []runtime.LogicalModel{{ID: "logical", Alias: "chat", RoutePolicyID: "route"}},
	}))
	backend := NewControlBackend(nil, finopsAccounting{requests: []accounting.RequestRecord{
		{ProjectID: "project-b", LogicalModel: "chat", InputTokens: 10, OutputTokens: 5, ProviderCost: &costB, Source: "semantic_cache", SnapshotVersion: 1},
		{ProjectID: "project-a", LogicalModel: "chat", InputTokens: 20, OutputTokens: 10, RetryCount: 1, FallbackCount: 1, ProviderCost: &costA, Source: "gateway"},
	}}, nil, nil, nil, registry, nil, nil, nil, nil)

	view, err := backend.FinOps(context.Background(), tenancy.TenantScope{TenantID: "tenant"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Requests != 2 || view.Spend < 0.39 || view.SemanticHits != 1 || view.CacheHitRate != 0.5 || view.SemanticHitRate != 0.5 {
		t.Fatalf("view = %+v", view)
	}
	wantSavings, _ := pricing.Price(pricing.LiteReferencePriceVersion, "gpt-4o-mini", "USD", 10, 5)
	if view.SavedTokens != 15 || view.PricedSavedTokens != 15 || view.UnpricedSavedTokens != 0 || view.CacheSavings != wantSavings || view.EstimateVersion != pricing.LiteReferencePriceVersion.ID {
		t.Fatalf("cache savings = saved %d estimated %.6f", view.SavedTokens, view.CacheSavings)
	}
	if len(view.ByProject) != 2 || view.ByProject[0].ProjectID != "project-a" {
		t.Fatalf("projects = %+v", view.ByProject)
	}
	if len(view.ByModel) != 1 || view.ByModel[0].SavedTokens != 15 || view.ByModel[0].CacheSavings != wantSavings || view.ByModel[0].CacheHitRate != 0.5 {
		t.Fatalf("models = %+v", view.ByModel)
	}
	if view.RetryCost == 0 || view.FallbackCost == 0 || len(view.Recommendations) == 0 {
		t.Fatalf("optimization = retry %.4f fallback %.4f recommendations %+v", view.RetryCost, view.FallbackCost, view.Recommendations)
	}
}

func TestControlBackendFinOpsLeavesAmbiguousCacheSavingsUnpriced(t *testing.T) {
	registry := &runtime.ActiveRegistry{}
	registry.ActivateTenant("tenant-ref", runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "tenant-ref", Status: "active", Version: 2,
		Deployments: []runtime.Deployment{
			{ID: "cheap", Status: "enabled", UpstreamModel: "gpt-4o-mini"},
			{ID: "premium", Status: "enabled", UpstreamModel: "gpt-4o"},
		},
		RoutePolicies: []runtime.RoutePolicy{{ID: "route", DeploymentIDs: []string{"cheap", "premium"}}},
		LogicalModels: []runtime.LogicalModel{{ID: "logical", Alias: "chat", RoutePolicyID: "route"}},
	}))
	backend := NewControlBackend(nil, finopsAccounting{requests: []accounting.RequestRecord{{
		ProjectID: "project", LogicalModel: "chat", InputTokens: 100, OutputTokens: 20, Source: "cache", SnapshotVersion: 2,
	}}}, nil, nil, nil, registry, nil, nil, nil, nil)

	view, err := backend.FinOps(context.Background(), tenancy.TenantScope{TenantID: "tenant"})
	if err != nil {
		t.Fatal(err)
	}
	if view.SavedTokens != 120 || view.PricedSavedTokens != 0 || view.UnpricedSavedTokens != 120 || view.CacheSavings != 0 {
		t.Fatalf("view=%+v", view)
	}
	if len(view.Recommendations) == 0 || !strings.Contains(view.Recommendations[0].Detail, "120 unpriced") {
		t.Fatalf("recommendations=%+v", view.Recommendations)
	}
}

func TestControlBackendSimulatorUsesRecentLedgerMetrics(t *testing.T) {
	cheap := 0.01
	expensive := 0.20
	registry := &runtime.ActiveRegistry{}
	registry.ActivateTenant("ref", runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Status: "active", Version: 1,
		Projects:    []runtime.Project{{ID: "project", Status: "active"}},
		Providers:   []runtime.Provider{{ID: "provider", Status: "enabled"}},
		Credentials: []runtime.Credential{{ID: "credential", ProviderID: "provider", Status: "enabled"}},
		Deployments: []runtime.Deployment{
			{ID: "cheap", ProviderID: "provider", CredentialID: "credential", Status: "enabled", Capabilities: []string{"chat"}},
			{ID: "premium", ProviderID: "provider", CredentialID: "credential", Status: "enabled", Capabilities: []string{"chat"}},
		},
		RoutePolicies: []runtime.RoutePolicy{{ID: "route", ProjectID: "project", Strategy: "soft", DeploymentIDs: []string{"cheap", "premium"}, ScoreWeights: map[string]float64{"cost": 1, "latency": 0, "load": 0, "cache": 0}}},
		LogicalModels: []runtime.LogicalModel{{ID: "logical", Alias: "chat", RoutePolicyID: "route"}},
	}))
	backend := NewControlBackend(nil, finopsAccounting{requests: []accounting.RequestRecord{
		{DeploymentID: "cheap", ProviderCost: &cheap, LatencyMS: 200, Source: "gateway"},
		{DeploymentID: "premium", ProviderCost: &expensive, LatencyMS: 20, Source: "gateway"},
	}}, nil, nil, nil, registry, nil, nil, nil, nil)

	result, err := backend.Simulate(context.Background(), tenancy.TenantScope{TenantID: "tenant"}, SimulateRequest{Model: "chat", ProjectID: "project"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Selected != "cheap" {
		t.Fatalf("result = %+v", result)
	}
}

func TestControlBackendRecommendationsExposeEvidenceBackedSuggestions(t *testing.T) {
	cost := 0.30
	backend := NewControlBackend(nil, finopsAccounting{requests: []accounting.RequestRecord{{ProjectID: "project", LogicalModel: "chat", ProviderCost: &cost}}}, nil, nil, nil, nil, nil, nil, nil, nil)

	items, err := backend.Recommendations(context.Background(), tenancy.TenantScope{TenantID: "tenant"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Samples != 1 || !items[0].Acceptable || len(items[0].Change) == 0 {
		t.Fatalf("recommendations = %+v", items)
	}
}

func TestControlBackendSimulatorSurfacesPlanErrors(t *testing.T) {
	registry := &runtime.ActiveRegistry{}
	registry.ActivateTenant("ref", runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Status: "active", Version: 1,
		Projects:      []runtime.Project{{ID: "project", Status: "active"}},
		Providers:     []runtime.Provider{{ID: "provider", Status: "enabled"}},
		Credentials:   []runtime.Credential{{ID: "credential", ProviderID: "provider", Status: "enabled"}},
		Deployments:   []runtime.Deployment{{ID: "deploy", ProviderID: "provider", CredentialID: "credential", Status: "enabled", Capabilities: []string{"chat"}}},
		RoutePolicies: []runtime.RoutePolicy{{ID: "route", ProjectID: "project", Strategy: "priority", DeploymentIDs: []string{"deploy"}}},
		LogicalModels: []runtime.LogicalModel{{ID: "logical", Alias: "chat", RoutePolicyID: "route"}},
	}))
	backend := NewControlBackend(nil, nil, nil, nil, nil, registry, nil, nil, nil, nil)

	// A model that is not part of the snapshot must surface as an error instead
	// of a successful-but-empty simulation result.
	result, err := backend.Simulate(context.Background(), tenancy.TenantScope{TenantID: "tenant"}, SimulateRequest{Model: "missing-model", ProjectID: "project"})
	if err == nil {
		t.Fatalf("Simulate with unknown model = %+v, want error", result)
	}
	if result.Selected != "" {
		t.Fatalf("Simulate returned a selection despite failing: %+v", result)
	}
}

func TestControlBackendLiveTailPublishesSummaries(t *testing.T) {
	bus := NewLiveBus()
	backend := NewControlBackend(nil, nil, nil, nil, nil, nil, nil, bus, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	events, err := backend.LiveTail(ctx, tenancy.TenantScope{TenantID: "tenant"})
	if err != nil {
		t.Fatal(err)
	}
	bus.Publish(LiveEvent{RequestID: "req-1", Outcome: "success", DeploymentID: "d1", LatencyMS: 12})
	select {
	case event := <-events:
		if event.RequestID != "req-1" || event.LatencyMS != 12 {
			t.Fatalf("event = %+v", event)
		}
	case <-ctx.Done():
		t.Fatal("no live event received")
	}
}

func TestControlBackendMeAndCreateAlertRule(t *testing.T) {
	store := newMemoryAlertStore()
	backend := NewControlBackend(nil, nil, nil, nil, store, nil, nil, nil, nil, nil)
	me, err := backend.Me(context.Background(), tenancy.TenantScope{TenantID: "tenant"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(me)
	if len(raw) == 0 {
		t.Fatal("Me() returned empty")
	}
	ruleInput, _ := json.Marshal(map[string]any{
		"name": "budget-alert", "ruleType": "budget", "metric": "budget.usage",
		"operator": "gt", "threshold": 100, "severity": "high", "enabled": true,
	})
	rule, err := backend.CreateAlertRule(context.Background(), tenancy.TenantScope{TenantID: "tenant"}, ruleInput, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if rule.ID == "" || rule.Name != "budget-alert" || rule.Threshold != 100 {
		t.Fatalf("rule = %+v", rule)
	}
}

func TestControlBackendImportDefaultAlertRulesIsIdempotent(t *testing.T) {
	store := newMemoryAlertStore()
	backend := NewControlBackend(nil, nil, nil, nil, store, nil, nil, nil, nil, nil)
	scope := tenancy.TenantScope{TenantID: "tenant"}
	first, err := backend.ImportDefaultAlertRules(context.Background(), scope, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == 0 || first[0].ID == "" {
		t.Fatalf("first import = %+v", first)
	}
	second, err := backend.ImportDefaultAlertRules(context.Background(), scope, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 0 {
		t.Fatalf("second import = %+v, want idempotent no-op", second)
	}
}

func TestControlBackendNotificationSettingsCRUDAndValidation(t *testing.T) {
	store := newMemoryAlertStore()
	backend := NewControlBackend(nil, nil, nil, nil, store, nil, nil, nil, nil, nil)
	scope := tenancy.TenantScope{TenantID: "tenant"}

	initial, err := backend.NotificationSettings(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Enabled || initial.WebhookURL != "" || initial.RequiresRestart {
		t.Fatalf("initial settings = %+v", initial)
	}
	if _, err := backend.UpdateNotificationSettings(context.Background(), scope, NotificationSettingsInput{WebhookURL: "http://user:pass@example.com/hook", Enabled: true}, "admin"); err == nil {
		t.Fatal("accepted webhook URL with userinfo")
	}
	if _, err := backend.UpdateNotificationSettings(context.Background(), scope, NotificationSettingsInput{WebhookURL: "http://10.0.0.1/hook", Enabled: true}, "admin"); err == nil {
		t.Fatal("accepted blocked private webhook URL")
	}
	updated, err := backend.UpdateNotificationSettings(context.Background(), scope, NotificationSettingsInput{WebhookURL: "https://example.com/hook", Enabled: true}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Enabled || updated.WebhookURL != "https://example.com/hook" || updated.RequiresRestart {
		t.Fatalf("updated settings = %+v", updated)
	}
	if len(updated.Targets) != 1 || updated.Targets[0].URL != "https://example.com/hook" || updated.Targets[0].MinSeverity != alert.SeverityLow {
		t.Fatalf("synthesized legacy target = %+v", updated.Targets)
	}
	if updated.DedupSeconds != 300 {
		t.Fatalf("default dedup = %d", updated.DedupSeconds)
	}
	if err := backend.DeleteNotificationSettings(context.Background(), scope, "admin"); err != nil {
		t.Fatal(err)
	}
	afterDelete, err := backend.NotificationSettings(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if afterDelete.Enabled || afterDelete.WebhookURL != "" {
		t.Fatalf("after delete = %+v", afterDelete)
	}
}

func TestControlBackendNotificationTargetsSeverityRouting(t *testing.T) {
	store := newMemoryAlertStore()
	backend := NewControlBackend(nil, nil, nil, nil, store, nil, nil, nil, nil, nil)
	scope := tenancy.TenantScope{TenantID: "tenant"}

	if _, err := backend.UpdateNotificationSettings(context.Background(), scope, NotificationSettingsInput{
		Targets: []NotificationTargetView{
			{URL: "https://example.com/low", MinSeverity: "low"},
			{URL: "https://example.com/high", MinSeverity: "high"},
		},
		DedupSeconds: 60,
		Enabled:      true,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	view, err := backend.NotificationSettings(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Targets) != 2 || view.Targets[0].MinSeverity != "low" || view.Targets[1].MinSeverity != "high" || view.DedupSeconds != 60 {
		t.Fatalf("targets = %+v", view.Targets)
	}
	if view.WebhookURL != "https://example.com/low" {
		t.Fatalf("primary webhookUrl = %q", view.WebhookURL)
	}
}

func TestControlBackendNotificationTargetsValidation(t *testing.T) {
	store := newMemoryAlertStore()
	backend := NewControlBackend(nil, nil, nil, nil, store, nil, nil, nil, nil, nil)
	scope := tenancy.TenantScope{TenantID: "tenant"}

	if _, err := backend.UpdateNotificationSettings(context.Background(), scope, NotificationSettingsInput{
		Targets: []NotificationTargetView{{URL: "https://example.com/hook", MinSeverity: "urgent"}},
		Enabled: true,
	}, "admin"); err == nil {
		t.Fatal("accepted invalid min severity")
	}
	if _, err := backend.UpdateNotificationSettings(context.Background(), scope, NotificationSettingsInput{
		Targets: []NotificationTargetView{{URL: "http://user:pass@example.com/hook", MinSeverity: "high"}},
		Enabled: true,
	}, "admin"); err == nil {
		t.Fatal("accepted target with credentials")
	}
}

func TestControlBackendNotificationSettingsReloadsRuntime(t *testing.T) {
	store := newMemoryAlertStore()
	backend := NewControlBackend(nil, nil, nil, nil, store, nil, nil, nil, nil, nil)
	scope := tenancy.TenantScope{TenantID: "tenant"}
	var reloadedSettings alert.NotificationSettings
	backend.SetNotificationReloader(func(_ context.Context, settings alert.NotificationSettings) error {
		reloadedSettings = settings
		return nil
	})
	if _, err := backend.UpdateNotificationSettings(context.Background(), scope, NotificationSettingsInput{WebhookURL: "https://example.com/hook", Enabled: true}, "admin"); err != nil {
		t.Fatal(err)
	}
	if reloadedSettings.WebhookURL != "https://example.com/hook" || !reloadedSettings.Enabled {
		t.Fatalf("reload update = %+v", reloadedSettings)
	}
	if err := backend.DeleteNotificationSettings(context.Background(), scope, "admin"); err != nil {
		t.Fatal(err)
	}
	if reloadedSettings.TenantID != "tenant" || reloadedSettings.Enabled {
		t.Fatalf("reload delete = %+v", reloadedSettings)
	}
}
