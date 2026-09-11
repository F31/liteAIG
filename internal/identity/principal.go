package identity

// AttributionTrust levels for usage attribution.
const (
	AttributionVerified  = "verified"
	AttributionDelegated = "delegated"
	AttributionKeyBound  = "key_bound"
	AttributionUntrusted = "untrusted"
	AttributionNone      = "none"
)

const (
	PrincipalUser           = "user"
	PrincipalApplication    = "application"
	PrincipalServiceAccount = "service_account"
	PrincipalAgent          = "agent"
	PrincipalAPIKey         = "api_key"
	PrincipalSystem         = "system"
)

// AuthMethod values for federated transports.
const (
	AuthMethodAPIKey           = "api_key"
	AuthMethodDelegatedJWT     = "delegated_jwt"
	AuthMethodOIDC             = "oidc"
	AuthMethodServiceAccount   = "service_account"
	AuthMethodFederatedMTLS    = "federated_mtls"
	AuthMethodFederatedOIDC    = "federated_oidc"
	AuthMethodFederatedJWS     = "federated_jws"
	AuthMethodRegistryAttested = "registry_attested"
)

// TrustBoundary values carried on the canonical principal.
const (
	BoundaryInternal          = "internal"
	BoundaryExternalFederated = "external_federated"
)

// TrustedAttribution reports whether a trust level authorizes User/OrgUnit
// budget and attribution enforcement.
func TrustedAttribution(trust string) bool {
	return trust == AttributionVerified || trust == AttributionDelegated
}

type Principal struct {
	Type, TenantID, ProjectID, APIKeyID      string
	ApplicationID, AgentID, ServiceAccountID string
	AuthMethod, AttributionTrust             string

	// Federated identity fields (V8.2 cross-organization governance).
	TrustBoundary            string
	FederationRelationshipID string
	ExternalSubject          string
}
