package golden

import (
	"context"
	"testing"

	"github.com/F31/liteAIG/internal/federation"
	"github.com/F31/liteAIG/internal/finops/pricing"
	"github.com/F31/liteAIG/internal/tenancy"
)

func TestScenarioFFederatedAgentCollaborationGovernance(t *testing.T) {
	scope := tenancy.TenantScope{TenantID: "org-a"}
	lifecycle := federation.NewLifecycle(nil)

	// Discovery never trusts: a discovered partner agent is a candidate only.
	discovered, err := lifecycle.Discover(context.Background(), federation.Relationship{
		ID: "rel-1", TenantID: "org-a", ExternalAgentID: "partner-b", Name: "Partner B",
		ExternalSubject: "partner-b.example", AuthMethod: "federated_mtls",
	})
	if err != nil || discovered.Status != federation.StatusCandidate {
		t.Fatalf("discover = %+v, %v", discovered, err)
	}
	if lifecycle.Active(scope, "rel-1") {
		t.Fatal("discovered relationship must not be active")
	}

	// Unverified anchor cannot activate.
	unverified := discovered
	unverified.Anchors = []federation.TrustAnchor{{ID: "a1", Type: federation.AnchorJWS, Verified: false}}
	if _, err := lifecycle.Activate(context.Background(), unverified); err == nil {
		t.Fatal("unverified anchor activated")
	}

	// Verified anchor + grants + data boundary activate.
	relationship := discovered
	relationship.Anchors = []federation.TrustAnchor{{ID: "a1", Type: federation.AnchorMTLS, Subject: "spki:partner-b", Verified: true}}
	relationship.ProjectGrants = []federation.ProjectGrant{{ID: "pg", ProjectID: "project-a"}}
	relationship.CapabilityGrants = []federation.CapabilityGrant{{ID: "cg", Capability: "invoice.read"}}
	relationship.DataBoundary = federation.DataBoundary{Status: federation.BoundaryContractuallyBound, ProcessingRegions: []string{"eu"}}
	if _, err := lifecycle.Activate(context.Background(), relationship); err != nil {
		t.Fatalf("activate = %v", err)
	}

	// Data boundary: a stricter project requirement rejects.
	if _, ok := lifecycle.Get(context.Background(), scope, "rel-1"); !ok {
		t.Fatal("relationship missing after activation")
	}
	activeRel, _ := lifecycle.Get(context.Background(), scope, "rel-1")
	if err := enforceScenarioFBoundary(activeRel, federation.BoundaryContractuallyBound, []string{"us"}); err == nil {
		t.Fatal("processing region conflict accepted")
	}

	// External cost dual-budget: procurement cannot bypass task total.
	budget := pricing.ExternalProcurementBudget{RelationshipID: "rel-1", Limit: 100}
	external := pricing.ExternalProcurement{RelationshipID: "rel-1", VendorID: "partner-b", ExternalCost: 20}
	if err := budget.Admit(external, 100, 10); err != nil {
		t.Fatalf("both budgets should pass: %v", err)
	}
	// Task total near its limit: procurement headroom must not relax it.
	if err := budget.Admit(external, 20, 10); err == nil {
		t.Fatal("procurement headroom bypassed the task total budget")
	}

	// Sticky crosses_trust_boundary.
	task := &federation.TaskState{}
	task.RecordCrossing()
	task.RecordCrossing()
	if !task.CrossedTrustBoundary() {
		t.Fatal("crosses_trust_boundary must be sticky")
	}

	// Loop detection terminates the chain.
	detector := federation.NewLoopDetector(4)
	detector.Enter("agent-a")
	detector.Exit()
	if !detector.Enter("agent-b") || !detector.Enter("partner-b") {
		t.Fatal("first hops should be allowed")
	}
	if detector.Enter("partner-b") {
		t.Fatal("agent loop must be terminated")
	}
}

func enforceScenarioFBoundary(relationship federation.Relationship, min federation.DataBoundaryStatus, allowedRegions []string) error {
	if relationship.DataBoundary.Status != min {
		return federation.ErrDataBoundary
	}
	for _, allowed := range allowedRegions {
		for _, region := range relationship.DataBoundary.ProcessingRegions {
			if region == allowed {
				return nil
			}
		}
	}
	return federation.ErrDataBoundary
}
