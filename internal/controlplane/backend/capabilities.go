package backend

import (
	"context"
	"encoding/json"
	"time"

	"github.com/F31/liteAIG/internal/controlplane/config"
	"github.com/F31/liteAIG/internal/finops/accounting"
	guardrailbench "github.com/F31/liteAIG/internal/guardrail/benchmark"
	"github.com/F31/liteAIG/internal/identity/apikey"
	"github.com/F31/liteAIG/internal/tenancy"
)

// Capability interfaces group the Backend surface by the route family that
// consumes it. Handlers depend on the narrowest capability they need;
// Backend composes all of them so existing implementations keep working
// until each family is migrated to its own wiring.
type (
	// SetupService initializes a fresh tenant through the wizard.
	SetupService interface {
		Setup(context.Context, json.RawMessage) (WizardSetupResponse, error)
	}

	// ProjectService lists and creates tenant projects.
	ProjectService interface {
		Projects(context.Context, tenancy.TenantScope) ([]ProjectSummary, error)
		CreateProject(context.Context, tenancy.TenantScope, ProjectCreateInput) (ProjectSummary, error)
	}

	// TenantService manages tenant lifecycle for the self-hosted system operator.
	TenantService interface {
		Tenants(context.Context) ([]TenantSummary, error)
		CreateTenant(context.Context, TenantCreateInput, string) (TenantSummary, error)
		UpdateTenantStatus(context.Context, string, TenantStatusInput, string) (TenantSummary, error)
	}

	// AuditRetentionService manages the tenant audit retention policy.
	AuditRetentionService interface {
		GetAuditRetention(context.Context, tenancy.TenantScope) (AuditRetentionView, error)
		SetAuditRetention(context.Context, tenancy.TenantScope, AuditRetentionInput, string) (AuditRetentionView, error)
	}

	// DashboardService serves the aggregated operational views.
	DashboardService interface {
		Dashboard(context.Context, tenancy.TenantScope) (DashboardView, error)
		FinOps(context.Context, tenancy.TenantScope) (FinOpsView, error)
		Recommendations(context.Context, tenancy.TenantScope) ([]RecommendationView, error)
		Health(context.Context, tenancy.TenantScope) (HealthView, error)
		LiveTail(context.Context, tenancy.TenantScope) (<-chan LiveEvent, error)
	}

	// KeyService manages API keys and provider credentials.
	KeyService interface {
		CreateKey(context.Context, tenancy.TenantScope, apikey.CreateInput, string) (*apikey.CreateResult, error)
		RevokeKey(context.Context, tenancy.TenantScope, string, string) error
		ListKeys(context.Context, tenancy.TenantScope) ([]KeyResource, error)
		RevealKey(context.Context, tenancy.TenantScope, string) (string, error)
		DiscoverMCPTools(context.Context, tenancy.TenantScope, string) ([]DiscoveredTool, error)
	}

	// ConfigService manages the draft/validate/publish configuration loop.
	ConfigService interface {
		Drafts(context.Context, tenancy.TenantScope) ([]DraftSummary, error)
		CreateDraft(context.Context, tenancy.TenantScope, string) (*config.Draft, error)
		GetDraft(context.Context, tenancy.TenantScope, string) (*config.Draft, error)
		UpdateDraft(context.Context, tenancy.TenantScope, string, int64, config.TenantConfig, string) (*config.Draft, error)
		Diff(context.Context, tenancy.TenantScope, string) ([]config.DiffEntry, error)
		Publish(context.Context, tenancy.TenantScope, string, int64, string) (*config.Version, []config.Diagnostic, error)
		Rollback(context.Context, tenancy.TenantScope, int64, string) (*config.Version, error)
		Versions(context.Context, tenancy.TenantScope) ([]config.Version, error)
		Rebase(context.Context, tenancy.TenantScope, string, string) (RebaseResult, error)
	}

	// RuntimeService reports the live runtime and resource catalog.
	RuntimeService interface {
		Runtime(context.Context, tenancy.TenantScope) (RuntimeResources, error)
		ResourceCatalog(context.Context, tenancy.TenantScope) (ResourceCatalogView, error)
	}

	// PlaygroundService executes test requests against the tenant pipeline.
	PlaygroundService interface {
		Playground(context.Context, tenancy.TenantScope, PlaygroundRequest) (PlaygroundResponse, error)
	}

	// RequestService inspects recorded requests, usage and audit trails.
	RequestService interface {
		Request(context.Context, tenancy.TenantScope, string) (*accounting.RequestRecord, error)
		Requests(context.Context, tenancy.TenantScope, int) ([]accounting.RequestRecord, error)
		Usage(context.Context, tenancy.TenantScope, string) (*accounting.UsageRecord, error)
		Audit(context.Context, tenancy.TenantScope, int, int) ([]AuditRecord, error)
		ToolCalls(context.Context, tenancy.TenantScope, int) ([]ToolCallView, error)
	}

	// AlertService manages alerting rules and circuit breakers.
	AlertService interface {
		Alerts(context.Context, tenancy.TenantScope, string) ([]AlertView, error)
		AlertRules(context.Context, tenancy.TenantScope) ([]RuleView, error)
		CreateAlertRule(context.Context, tenancy.TenantScope, json.RawMessage, string) (RuleView, error)
		ImportDefaultAlertRules(context.Context, tenancy.TenantScope, string) ([]RuleView, error)
		NotificationSettings(context.Context, tenancy.TenantScope) (NotificationSettingsView, error)
		UpdateNotificationSettings(context.Context, tenancy.TenantScope, NotificationSettingsInput, string) (NotificationSettingsView, error)
		DeleteNotificationSettings(context.Context, tenancy.TenantScope, string) error
		AlertAction(context.Context, tenancy.TenantScope, string, string, string) error
		ResetCircuit(context.Context, tenancy.TenantScope, string, string, string) error
	}

	// SecurityService exposes security events and the policy simulator.
	SecurityService interface {
		SecurityEvents(context.Context, tenancy.TenantScope, int) ([]SecurityEventView, error)
		Simulate(context.Context, tenancy.TenantScope, SimulateRequest) (SimulateResult, error)
	}

	// DelegationService manages identity-owned agent delegation grants.
	DelegationService interface {
		Delegations(context.Context, tenancy.TenantScope) ([]DelegationGrantView, error)
		GrantDelegation(context.Context, tenancy.TenantScope, DelegationGrantInput, string) (DelegationGrantView, error)
		RevokeDelegation(context.Context, tenancy.TenantScope, string, string) error
	}

	// FederationService covers cross-tenant relationships and approvals.
	FederationService interface {
		Federation(context.Context, tenancy.TenantScope) (FederationView, error)
		Approvals(context.Context, tenancy.TenantScope, int) ([]ApprovalView, error)
		ApprovalAction(context.Context, tenancy.TenantScope, string, string, string) error
		FederationSuspend(context.Context, tenancy.TenantScope, string, string) error
		FederationDiscover(context.Context, tenancy.TenantScope, FederationDiscoverInput) (RelationshipView, error)
		FederationReview(context.Context, tenancy.TenantScope, string, FederationReviewInput, string) error
		AgentGraph(context.Context, tenancy.TenantScope, string) (AgentGraphView, error)
		// PushDeliveries lists the tenant's most recent sanitized A2A push
		// callback deliveries for operator drill-down. status filters by
		// pending/sending/delivered/failed; empty selects all.
		PushDeliveries(context.Context, tenancy.TenantScope, string, int) ([]PushDeliveryView, error)
	}

	// IdentityService reports the session's own profile.
	IdentityService interface {
		Me(context.Context, tenancy.TenantScope) (MeView, error)
	}

	// EvidenceService exports a tenant's read-only evidence archive for a time
	// range: config/policy versions, RBAC assignments, audit events, guardrail
	// events, provider destinations, secret rotation, and federation history.
	// Prompt/response bodies and secrets are excluded by construction.
	EvidenceService interface {
		Evidence(context.Context, tenancy.TenantScope, time.Time, time.Time) (guardrailbench.Archive, error)
	}

	// SystemConfigService manages the singleton system-level configuration
	// (A4 tenant policy defaults). The Admin API gates both methods to the
	// system_admin role; this interface carries no scope.
	SystemConfigService interface {
		SystemConfig(context.Context) (config.SystemConfig, error)
		SetSystemConfig(context.Context, config.SystemConfig, string) (config.SystemConfig, error)
	}
)
