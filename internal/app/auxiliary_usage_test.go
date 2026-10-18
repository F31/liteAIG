package app

import (
	"context"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/finops/accounting"
	"github.com/F31/liteAIG/internal/finops/budget"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/platform/id"
	"github.com/F31/liteAIG/internal/tenancy"
)

type capturingAccountingRepo struct{ facts []accounting.Facts }

func (r *capturingAccountingRepo) Finalize(_ context.Context, facts accounting.Facts) (bool, error) {
	r.facts = append(r.facts, facts)
	return true, nil
}
func (r *capturingAccountingRepo) GetRequest(context.Context, tenancy.TenantScope, string) (*accounting.RequestRecord, error) {
	return nil, nil
}
func (r *capturingAccountingRepo) ListRequests(context.Context, tenancy.TenantScope, int) ([]accounting.RequestRecord, error) {
	return nil, nil
}
func (r *capturingAccountingRepo) GetUsage(context.Context, tenancy.TenantScope, string) (*accounting.UsageRecord, error) {
	return nil, nil
}

func TestAuxiliaryUsageIsLinkedToParentRequest(t *testing.T) {
	repo := &capturingAccountingRepo{}
	pipeline := &litePipeline{accounting: repo, ids: id.NewGenerator(nil), clock: systemClock{}}
	snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Version: 3,
		Providers:   []runtime.Provider{{ID: "provider", Status: "enabled"}},
		Deployments: []runtime.Deployment{{ID: "deployment", ProviderID: "provider", UpstreamModel: "gpt-4o-mini", Status: "enabled"}},
	})
	request := &kernel.RequestContext{
		RequestID: "main-request",
		Snapshot:  snapshot,
		Interaction: &interaction.Context{
			TenantID: "tenant", ProjectID: "project",
		},
		Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "default-chat"},
		Key:     runtime.APIKey{ID: "key-id", PublicID: "key-public"},
	}
	pipeline.recordAuxiliaryUsage(context.Background(), request, "embedding", "text-embedding-3-small", "deployment", interaction.UnifiedUsage{InputTokens: 12, OutputTokens: 0}, 5*time.Millisecond, "", "", 0, nil)
	if len(repo.facts) != 1 {
		t.Fatalf("facts = %d, want 1", len(repo.facts))
	}
	facts := repo.facts[0]
	if facts.ParentRequestID != "main-request" || facts.RequestID == "main-request" {
		t.Fatalf("parent/request ids = %q/%q", facts.ParentRequestID, facts.RequestID)
	}
	if facts.UsageSource != "auxiliary_embedding" || facts.InputTokens != 12 || facts.LogicalModel != "text-embedding-3-small" {
		t.Fatalf("facts = %+v", facts)
	}
	if facts.ProviderCost == nil {
		t.Fatal("auxiliary usage should include a reference provider cost when priced")
	}
}

func TestAuxiliaryUsageReservesAndSettlesBudget(t *testing.T) {
	repo := &capturingAccountingRepo{}
	pipeline := &litePipeline{accounting: repo, ids: id.NewGenerator(nil), clock: systemClock{}, budget: newBudgetEnforcer(budget.New(systemClock{}))}
	snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Version: 1,
		Projects:       []runtime.Project{{ID: "project", Status: "active"}},
		BudgetPolicies: []runtime.BudgetPolicy{{ID: "policy", TenantID: "tenant", ProjectID: "project", WindowHours: 1, TokenLimit: 100, Mode: "hard"}},
		Providers:      []runtime.Provider{{ID: "provider", Status: "enabled"}},
		Deployments:    []runtime.Deployment{{ID: "deployment", ProviderID: "provider", UpstreamModel: "gpt-4o-mini", Status: "enabled"}},
	})
	request := &kernel.RequestContext{
		RequestID: "main-request", Snapshot: snapshot,
		Interaction: &interaction.Context{TenantID: "tenant", ProjectID: "project"},
		Request:     &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "default-chat"},
	}
	policyID, reservationID, estimated, err := pipeline.reserveAuxiliary(request, 10)
	if err != nil || policyID == "" || reservationID == "" || estimated != 10 {
		t.Fatalf("reserve = %q/%q/%d/%v", policyID, reservationID, estimated, err)
	}
	pipeline.recordAuxiliaryUsage(context.Background(), request, "judge", "gpt-4o-mini", "deployment", interaction.UnifiedUsage{InputTokens: 4, OutputTokens: 1}, time.Millisecond, policyID, reservationID, estimated, nil)
	if len(repo.facts) != 1 {
		t.Fatalf("facts = %d", len(repo.facts))
	}
	facts := repo.facts[0]
	if facts.BudgetPolicyID != policyID || facts.ReservationID != reservationID || facts.ActualTokens == nil || *facts.ActualTokens != 5 {
		t.Fatalf("facts = %+v", facts)
	}
}
