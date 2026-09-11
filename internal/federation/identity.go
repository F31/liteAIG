// Package federation owns cross-organization Agent trust.
package federation

import (
	"context"
	"errors"

	"github.com/F31/liteAIG/internal/identity"
	"github.com/F31/liteAIG/internal/tenancy"
)

// TransportFacts are the transport-layer credentials of an inbound call.
type TransportFacts struct {
	AuthMethod string // federated_mtls | federated_oidc | federated_jws | registry_attested
	Subject    string // mTLS SPKI subject / OIDC subject / JWS issuer+subject / registry attestation id
	Issuer     string // OIDC issuer or registry
}

// Resolver maps inbound transport credentials to a Relationship and yields a
// Federated Principal. Remote payload self-claims are never accepted here.
type Resolver struct {
	lifecycle *Lifecycle
}

// NewResolver builds an inbound identity resolver over the relationship store.
func NewResolver(lifecycle *Lifecycle) *Resolver {
	return &Resolver{lifecycle: lifecycle}
}

// Resolve finds the active relationship whose anchor subject matches the
// transport facts and returns a Federated Principal. It returns the
// relationship id and external agent id so authorization can be bounded by the
// relationship grants.
func (r *Resolver) Resolve(ctx context.Context, scope tenancy.TenantScope, facts TransportFacts) (*identity.Principal, error) {
	if facts.AuthMethod == "" || facts.Subject == "" {
		return nil, errors.New("inbound transport identity missing")
	}
	relationships := r.allForTenant(ctx, scope)
	for _, relationship := range relationships {
		if relationship.Status != StatusActive {
			continue
		}
		if r.matches(relationship, facts) {
			return &identity.Principal{
				Type:                     identity.PrincipalAgent,
				TenantID:                 scope.TenantID,
				AgentID:                  relationship.ExternalAgentID,
				AuthMethod:               facts.AuthMethod,
				AttributionTrust:         identity.AttributionDelegated,
				TrustBoundary:            identity.BoundaryExternalFederated,
				FederationRelationshipID: relationship.ID,
				ExternalSubject:          facts.Subject,
			}, nil
		}
	}
	return nil, errors.New("no active relationship matches inbound transport identity")
}

func (r *Resolver) allForTenant(ctx context.Context, scope tenancy.TenantScope) []Relationship {
	r.lifecycle.mu.Lock()
	defer r.lifecycle.mu.Unlock()
	var result []Relationship
	for _, relationship := range r.lifecycle.states {
		if relationship.TenantID == scope.TenantID {
			result = append(result, relationship)
		}
	}
	return result
}

func (r *Resolver) matches(relationship Relationship, facts TransportFacts) bool {
	for _, anchor := range relationship.Anchors {
		if !anchor.Verified {
			continue
		}
		matchesSubject := anchor.Subject == facts.Subject
		if facts.Issuer != "" {
			matchesSubject = matchesSubject || anchor.Subject == facts.Issuer+":"+facts.Subject
		}
		if matchesSubject {
			return true
		}
	}
	return false
}
