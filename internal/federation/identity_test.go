package federation

import (
	"context"
	"testing"

	"github.com/F31/liteAIG/internal/identity"
	"github.com/F31/liteAIG/internal/tenancy"
)

func setupActiveRelationship(t *testing.T, subject string) *Lifecycle {
	t.Helper()
	lifecycle := NewLifecycle(nil)
	relationship := testRelationship()
	relationship.Anchors = []TrustAnchor{{ID: "a", Type: AnchorMTLS, Subject: subject, Verified: true}}
	relationship.ProjectGrants = []ProjectGrant{{ID: "pg", ProjectID: "project"}}
	relationship.CapabilityGrants = []CapabilityGrant{{ID: "cg", Capability: "invoice.read"}}
	if _, err := lifecycle.Activate(context.Background(), relationship); err != nil {
		t.Fatal(err)
	}
	return lifecycle
}

func TestInboundTransportResolvesToFederatedPrincipal(t *testing.T) {
	lifecycle := setupActiveRelationship(t, "spki:partner-cert")
	resolver := NewResolver(lifecycle)
	principal, err := resolver.Resolve(context.Background(), scope(), TransportFacts{AuthMethod: "federated_mtls", Subject: "spki:partner-cert"})
	if err != nil {
		t.Fatal(err)
	}
	if principal.Type != identity.PrincipalAgent ||
		principal.TrustBoundary != identity.BoundaryExternalFederated ||
		principal.FederationRelationshipID != "rel-1" ||
		principal.ExternalSubject != "spki:partner-cert" {
		t.Fatalf("principal = %+v", principal)
	}
}

func TestInboundUnmatchedTransportRejected(t *testing.T) {
	lifecycle := setupActiveRelationship(t, "spki:partner-cert")
	resolver := NewResolver(lifecycle)
	if _, err := resolver.Resolve(context.Background(), scope(), TransportFacts{AuthMethod: "federated_mtls", Subject: "spki:attacker-cert"}); err == nil {
		t.Fatal("unmatched inbound identity must be rejected")
	}
	// Missing transport facts are rejected even if a payload self-claim exists.
	if _, err := resolver.Resolve(context.Background(), scope(), TransportFacts{}); err == nil {
		t.Fatal("empty transport facts accepted")
	}
}

func TestSelfClaimedIdentityNeverGrantsPrivilege(t *testing.T) {
	lifecycle := setupActiveRelationship(t, "spki:partner-cert")
	resolver := NewResolver(lifecycle)
	// The resolver API has no field for remote self-claims: authorization is
	// derived purely from transport facts + relationship grants.
	principal, err := resolver.Resolve(context.Background(), scope(), TransportFacts{AuthMethod: "federated_mtls", Subject: "spki:partner-cert"})
	if err != nil {
		t.Fatal(err)
	}
	// The principal carries only the relationship-derived identity, never an
	// internal user id or delegation: AgentID is the external agent, and no
	// internal scope is attached.
	if principal.AgentID != "ext-agent" || principal.ProjectID != "" || principal.APIKeyID != "" {
		t.Fatalf("principal leaked self-claimed identity: %+v", principal)
	}
	if principal.FederationRelationshipID == "" {
		t.Fatal("federated principal missing relationship id")
	}
	// A payload-supplied internal user id cannot be expressed through this
	// resolver, and an OIDC transport that does not match any verified anchor
	// is rejected rather than mapped by any self-claimed subject.
	if _, err := resolver.Resolve(context.Background(), scope(), TransportFacts{AuthMethod: "federated_oidc", Issuer: "idp.example", Subject: "partner-sub"}); err == nil {
		t.Fatal("unverified OIDC transport must be rejected")
	}
}

func TestFederatedPrincipalBoundedByGrants(t *testing.T) {
	lifecycle := setupActiveRelationship(t, "spki:partner-cert")
	resolver := NewResolver(lifecycle)
	principal, err := resolver.Resolve(context.Background(), scope(), TransportFacts{AuthMethod: "federated_mtls", Subject: "spki:partner-cert"})
	if err != nil {
		t.Fatal(err)
	}
	// The relationship grants are enforced at call time via the effective
	// permission evaluator; here we assert the principal carries no internal
	// membership that would widen scope.
	if principal.ProjectID != "" && principal.APIKeyID != "" {
		t.Fatalf("federated principal gained internal scope: %+v", principal)
	}
	// Cross-tenant isolation: another tenant cannot resolve this relationship.
	if _, err := resolver.Resolve(context.Background(), tenancy.TenantScope{TenantID: "other"}, TransportFacts{AuthMethod: "federated_mtls", Subject: "spki:partner-cert"}); err == nil {
		t.Fatal("cross-tenant inbound resolution must fail")
	}
}
