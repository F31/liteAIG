package adminapi

import "github.com/F31/liteAIG/internal/controlplane/backend"

type (
	SetupService          = backend.SetupService
	TenantService         = backend.TenantService
	ProjectService        = backend.ProjectService
	DashboardService      = backend.DashboardService
	KeyService            = backend.KeyService
	ConfigService         = backend.ConfigService
	RuntimeService        = backend.RuntimeService
	PlaygroundService     = backend.PlaygroundService
	RequestService        = backend.RequestService
	AlertService          = backend.AlertService
	SecurityService       = backend.SecurityService
	DelegationService     = backend.DelegationService
	FederationService     = backend.FederationService
	IdentityService       = backend.IdentityService
	EvidenceService       = backend.EvidenceService
	AuditRetentionService = backend.AuditRetentionService
	SystemConfigService   = backend.SystemConfigService
)

// fullCapabilities is satisfied by an object that provides every service the
// presentation layer can wire. It only parameterizes AllOf, so the type
// system enforces the claim at the call site without any runtime assertion.
type fullCapabilities interface {
	SetupService
	TenantService
	ProjectService
	DashboardService
	KeyService
	ConfigService
	RuntimeService
	PlaygroundService
	RequestService
	AlertService
	SecurityService
	DelegationService
	FederationService
	IdentityService
	EvidenceService
	AuditRetentionService
	CredentialOperator
	PlaygroundStreamer
	GuardrailPublisher
}

// Services is the narrow capability surface the presentation layer depends
// on. The composition root wires each capability independently, so a backend
// that only implements a subset can be served without stub methods.
type Services struct {
	Setup          SetupService
	Tenant         TenantService
	Project        ProjectService
	Dashboard      DashboardService
	Key            KeyService
	Config         ConfigService
	Runtime        RuntimeService
	Playground     PlaygroundService
	Request        RequestService
	Alert          AlertService
	Security       SecurityService
	Delegation     DelegationService
	Federation     FederationService
	Identity       IdentityService
	Evidence       EvidenceService
	AuditRetention AuditRetentionService

	// Optional capabilities: nil makes the dependent route answer
	// 501 NOT_IMPLEMENTED.
	Credentials      CredentialOperator
	PlaygroundStream PlaygroundStreamer
	Guardrails       GuardrailPublisher
}

// AllOf wires every capability to a single object. The compiler verifies the
// object implements each capability; no runtime assertion is involved.
func AllOf[T fullCapabilities](s T) Services {
	return Services{
		Setup: s, Tenant: s, Project: s, Dashboard: s, Key: s,
		Config: s, Runtime: s, Playground: s, Request: s,
		Alert: s, Security: s, Delegation: s, Federation: s, Identity: s,
		Evidence: s, AuditRetention: s, Credentials: s, PlaygroundStream: s, Guardrails: s,
	}
}
