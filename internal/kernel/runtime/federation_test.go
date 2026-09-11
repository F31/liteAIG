package runtime

import "testing"

func TestFederationIndexCompilesFromData(t *testing.T) {
	snapshot := NewTenantSnapshot(TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Version: 5,
		FederatedAgents: []FederatedAgent{
			{ID: "ext-1", Name: "Partner A", ExternalSubject: "partner-a.example", TrustBoundary: "external_federated", Status: "active"},
		},
		FederationRelationships: []FederationRelationship{
			{ID: "rel-1", ExternalAgentID: "ext-1", Status: "active", AssuranceLevel: "high", HasVerifiedAnchor: true},
		},
	})
	agent, ok := snapshot.FederatedAgent("ext-1")
	if !ok || agent.TrustBoundary != "external_federated" || agent.ExternalSubject != "partner-a.example" {
		t.Fatalf("federated agent = %+v", agent)
	}
	rel, ok := snapshot.FederationRelationship("rel-1")
	if !ok || !rel.HasVerifiedAnchor || rel.AssuranceLevel != "high" {
		t.Fatalf("relationship = %+v", rel)
	}
	if len(snapshot.FederatedAgents()) != 1 || len(snapshot.FederationRelationships()) != 1 {
		t.Fatalf("index sizes = %d/%d", len(snapshot.FederatedAgents()), len(snapshot.FederationRelationships()))
	}
	// Cross-tenant isolation is enforced at the store level (federation.Lifecycle);
	// the snapshot index is per-tenant by construction.
	if _, ok := snapshot.FederatedAgent("missing"); ok {
		t.Fatal("missing federated agent returned as present")
	}
}
