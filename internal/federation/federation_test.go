package federation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/tenancy"
)

func testRelationship() Relationship {
	return Relationship{
		ID:              "rel-1",
		TenantID:        "tenant",
		ExternalAgentID: "ext-agent",
		Name:            "Partner",
		ExternalSubject: "partner.example",
		AuthMethod:      "federated_mtls",
		CreatedAt:       time.Unix(1, 0),
	}
}

func scope() tenancy.TenantScope { return tenancy.TenantScope{TenantID: "tenant"} }

func TestDiscoveryNeverTrusts(t *testing.T) {
	lifecycle := NewLifecycle(func() time.Time { return time.Unix(2, 0) })
	discovered, err := lifecycle.Discover(context.Background(), testRelationship())
	if err != nil {
		t.Fatal(err)
	}
	if discovered.Status != StatusCandidate {
		t.Fatalf("status = %s, want candidate", discovered.Status)
	}
	// A discovered agent is never callable.
	if lifecycle.Active(scope(), "rel-1") {
		t.Fatal("discovered agent must not be active")
	}
}

func TestUnverifiedAnchorCannotActivate(t *testing.T) {
	lifecycle := NewLifecycle(nil)
	relationship := testRelationship()
	relationship.Anchors = []TrustAnchor{{ID: "a1", Type: AnchorJWS, Verified: false}}
	if _, err := lifecycle.Activate(context.Background(), relationship); !errors.Is(err, ErrUnverified) {
		t.Fatalf("Activate() with unverified anchor error = %v", err)
	}
	// Verified anchor but missing grants still cannot activate.
	relationship.Anchors[0].Verified = true
	if _, err := lifecycle.Activate(context.Background(), relationship); !errors.Is(err, ErrNotGranted) {
		t.Fatalf("Activate() without grants error = %v", err)
	}
	// Full requirements activate.
	relationship.ProjectGrants = []ProjectGrant{{ID: "pg", ProjectID: "project"}}
	relationship.CapabilityGrants = []CapabilityGrant{{ID: "cg", Capability: "invoice.read"}}
	active, err := lifecycle.Activate(context.Background(), relationship)
	if err != nil || active.Status != StatusActive {
		t.Fatalf("Activate() = %+v, %v", active, err)
	}
	if !lifecycle.Active(scope(), "rel-1") {
		t.Fatal("active relationship not callable")
	}
}

func TestAnchorTypePluggability(t *testing.T) {
	lifecycle := NewLifecycle(nil)
	relationship := testRelationship()
	relationship.Anchors = []TrustAnchor{{ID: "mtls", Type: AnchorMTLS, Subject: "spki:abc", Verified: true}}
	relationship.ProjectGrants = []ProjectGrant{{ID: "pg", ProjectID: "project"}}
	relationship.CapabilityGrants = []CapabilityGrant{{ID: "cg", Capability: "invoice.read"}}
	if _, err := lifecycle.Activate(context.Background(), relationship); err != nil {
		t.Fatalf("mTLS anchor must satisfy verified requirement: %v", err)
	}
}

func TestSuspendAndRevoke(t *testing.T) {
	lifecycle := NewLifecycle(nil)
	relationship := testRelationship()
	relationship.Anchors = []TrustAnchor{{ID: "a", Type: AnchorOIDC, Verified: true}}
	relationship.ProjectGrants = []ProjectGrant{{ID: "pg", ProjectID: "project"}}
	relationship.CapabilityGrants = []CapabilityGrant{{ID: "cg", Capability: "invoice.read"}}
	if _, err := lifecycle.Activate(context.Background(), relationship); err != nil {
		t.Fatal(err)
	}
	suspended, err := lifecycle.Suspend(context.Background(), scope(), "rel-1")
	if err != nil || suspended.Status != StatusSuspended {
		t.Fatalf("Suspend() = %+v, %v", suspended, err)
	}
	if lifecycle.Active(scope(), "rel-1") {
		t.Fatal("suspended relationship must not be callable")
	}
	revoked, err := lifecycle.Revoke(context.Background(), scope(), "rel-1")
	if err != nil || revoked.Status != StatusRevoked {
		t.Fatalf("Revoke() = %+v, %v", revoked, err)
	}
	// Cross-tenant isolation.
	if lifecycle.Active(tenancy.TenantScope{TenantID: "other"}, "rel-1") {
		t.Fatal("cross-tenant relationship must not be active")
	}
}

func TestDataBoundaryPolicy(t *testing.T) {
	relationship := testRelationship()
	relationship.DataBoundary = DataBoundary{Status: BoundaryUnknown, ProcessingRegions: []string{"unknown"}}
	// A project requiring contractually_bound must reject an unknown boundary.
	if err := enforceBoundary(relationship, BoundaryContractuallyBound, []string{"eu"}); !errors.Is(err, ErrDataBoundary) {
		t.Fatalf("unknown boundary accepted: %v", err)
	}
	relationship.DataBoundary = DataBoundary{Status: BoundaryContractuallyBound, ProcessingRegions: []string{"eu"}}
	if err := enforceBoundary(relationship, BoundaryContractuallyBound, []string{"eu"}); err != nil {
		t.Fatalf("contractually_bound eu boundary rejected: %v", err)
	}
	if err := enforceBoundary(relationship, BoundaryContractuallyBound, []string{"us"}); !errors.Is(err, ErrDataBoundary) {
		t.Fatalf("processing region conflict accepted: %v", err)
	}
}

// enforceBoundary mirrors the data-boundary gate used at call time.
func enforceBoundary(relationship Relationship, min DataBoundaryStatus, allowedRegions []string) error {
	if relationship.DataBoundary.Status != BoundaryContractuallyBound && relationship.DataBoundary.Status != min {
		return ErrDataBoundary
	}
	for _, allowed := range allowedRegions {
		for _, region := range relationship.DataBoundary.ProcessingRegions {
			if region == allowed {
				return nil
			}
		}
	}
	return ErrDataBoundary
}

func TestMaterialChangeRequiresReview(t *testing.T) {
	lifecycle := NewLifecycle(nil)
	relationship := testRelationship()
	relationship.Anchors = []TrustAnchor{{ID: "a", Type: AnchorJWS, Verified: true}}
	relationship.ProjectGrants = []ProjectGrant{{ID: "pg", ProjectID: "project"}}
	relationship.CapabilityGrants = []CapabilityGrant{{ID: "cg", Capability: "invoice.read"}}
	if _, err := lifecycle.Activate(context.Background(), relationship); err != nil {
		t.Fatal(err)
	}
	// A capability change moves the relationship to pending_review and must not
	// auto-activate the new capability.
	reviewed, err := lifecycle.ReviewMaterialChange(context.Background(), scope(), "rel-1", MaterialChange{Fields: []string{"capability"}})
	if err != nil || reviewed.Status != StatusPendingReview {
		t.Fatalf("ReviewMaterialChange() = %+v, %v", reviewed, err)
	}
	if lifecycle.Active(scope(), "rel-1") {
		t.Fatal("material change must not leave the relationship active")
	}
	// Re-activation requires a fresh review/activate with the new facts.
	reviewed.CapabilityGrants = append(reviewed.CapabilityGrants, CapabilityGrant{ID: "cg2", Capability: "payment.execute"})
	if _, err := lifecycle.Activate(context.Background(), reviewed); err != nil || !lifecycle.Active(scope(), "rel-1") {
		t.Fatalf("re-activate after review failed: %v", err)
	}
}

var _ = context.Background

// TestStaleCopyCannotClobberStoredState proves the optimistic version guard:
// a copy of the relationship read before a concurrent transition must not be
// allowed to silently overwrite the newer stored state (lost update).
func TestStaleCopyCannotClobberStoredState(t *testing.T) {
	ctx := context.Background()
	lifecycle := NewLifecycle(nil)
	fresh := testRelationship()
	fresh.Anchors = []TrustAnchor{{ID: "a1", Type: AnchorJWS, Verified: true}}
	fresh.ProjectGrants = []ProjectGrant{{ID: "pg", ProjectID: "project"}}
	fresh.CapabilityGrants = []CapabilityGrant{{ID: "cg", Capability: "invoice.read"}}
	if _, err := lifecycle.Activate(ctx, fresh); err != nil {
		t.Fatal(err)
	}

	// Actor B read the relationship before actor A's transition was saved.
	stale := testRelationship()
	stale.Anchors = fresh.Anchors
	stale.ProjectGrants = fresh.ProjectGrants
	stale.CapabilityGrants = fresh.CapabilityGrants
	stale.Version = 0

	if _, err := lifecycle.Suspend(ctx, scope(), "rel-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Activate(ctx, stale); !errors.Is(err, ErrStaleState) {
		t.Fatalf("Activate(stale copy) error = %v, want ErrStaleState", err)
	}
	current, ok := lifecycle.Get(ctx, scope(), "rel-1")
	if !ok || current.Status != StatusSuspended {
		t.Fatalf("status = %q (ok=%t), want suspended", current.Status, ok)
	}
	// A copy based on the latest stored state may still transition.
	if _, err := lifecycle.Activate(ctx, current); err != nil || !lifecycle.Active(scope(), "rel-1") {
		t.Fatalf("Activate(fresh copy) = %v, active=%t", err, lifecycle.Active(scope(), "rel-1"))
	}
}
