package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/F31/liteAIG/internal/catalog"
	"github.com/F31/liteAIG/internal/controlplane/config"
	controlrecommend "github.com/F31/liteAIG/internal/controlplane/recommend"
	"github.com/F31/liteAIG/internal/finops/accounting"
	"github.com/F31/liteAIG/internal/finops/aggregate"
	"github.com/F31/liteAIG/internal/finops/pricing"
	guardraildomain "github.com/F31/liteAIG/internal/guardrail"
	guardrailbench "github.com/F31/liteAIG/internal/guardrail/benchmark"
	"github.com/F31/liteAIG/internal/identity/apikey"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/observability/alert"
	"github.com/F31/liteAIG/internal/platform/egress"
	routing "github.com/F31/liteAIG/internal/routing/model"
	"github.com/F31/liteAIG/internal/tenancy"
	"net/url"
	"sort"
	"sync"
	"time"
)

// CircuitViewer exposes circuit state for Health and reset.
type CircuitViewer interface {
	State(deploymentID, credentialID string) string
	Reset(deploymentID, credentialID string)
}

// LiveBus fans request summaries to Live Tail subscribers (in-memory, per-process).
type LiveBus struct {
	mu      sync.Mutex
	clients map[chan LiveEvent]struct{}
}

func moneyText(value float64) string {
	return fmt.Sprintf("$%.2f", value)
}

func NewLiveBus() *LiveBus { return &LiveBus{clients: make(map[chan LiveEvent]struct{})} }

func (b *LiveBus) Subscribe() (chan LiveEvent, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan LiveEvent, 64)
	b.clients[ch] = struct{}{}
	return ch, func() {
		b.mu.Lock()
		delete(b.clients, ch)
		b.mu.Unlock()
		close(ch)
	}
}

func (b *LiveBus) Publish(event LiveEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- event:
		default: // drop on slow consumer; sampling is acceptable
		}
	}
}

// PublishLive adapts the kernel telemetry contract to the Live bus so the
// Data Plane depends only on contracts.TelemetrySink.
func (b *LiveBus) PublishLive(event contracts.LiveEvent) {
	b.Publish(LiveEvent{RequestID: event.RequestID, Outcome: event.Outcome, DeploymentID: event.DeploymentID, LatencyMS: event.LatencyMS})
}

var _ contracts.TelemetrySink = (*LiveBus)(nil)

// ControlBackend wires the Phase 1 control services behind the HTTP contract.
// The runnable Lite composition root additionally wires the capability slots
// below at startup; a control-plane-only profile leaves them unwired and the
// dependent routes answer errNotWired (HTTP 501).
type ControlBackend struct {
	configService      *config.Service
	accounting         accounting.Repository
	resourceCatalog    ResourceCatalogStore
	alertService       *alert.Service
	alertStore         alert.Store
	notificationStore  alert.NotificationSettingsStore
	registry           *runtime.ActiveRegistry
	circuits           CircuitViewer
	live               *LiveBus
	guardrails         *guardraildomain.PolicyRegistry
	securityEvents     func(context.Context, tenancy.TenantScope, int) ([]SecurityEventView, error)
	toolCalls          func(context.Context, tenancy.TenantScope, int) ([]ToolCallView, error)
	approvals          func(context.Context, tenancy.TenantScope, int) ([]ApprovalView, error)
	approvalAction     func(context.Context, tenancy.TenantScope, string, string, string) error
	federationSuspend  func(context.Context, tenancy.TenantScope, string, string) error
	federationDiscover func(context.Context, tenancy.TenantScope, FederationDiscoverInput) (RelationshipView, error)
	federationReview   func(context.Context, tenancy.TenantScope, string, FederationReviewInput, string) error
	agentGraph         func(context.Context, tenancy.TenantScope, string) (AgentGraphView, error)
	pushDeliveries     func(context.Context, tenancy.TenantScope, string, int) ([]PushDeliveryView, error)
	delegations        func(context.Context, tenancy.TenantScope) ([]DelegationGrantView, error)
	grantDelegation    func(context.Context, tenancy.TenantScope, DelegationGrantInput, string) (DelegationGrantView, error)
	revokeDelegation   func(context.Context, tenancy.TenantScope, string, string) error
	notificationReload func(context.Context, alert.NotificationSettings) error
	setupWizard        func(context.Context, json.RawMessage) (WizardSetupResponse, error)
	createKey          func(context.Context, tenancy.TenantScope, apikey.CreateInput, string) (*apikey.CreateResult, error)
	revokeKey          func(context.Context, tenancy.TenantScope, string, string) error
	listKeys           func(context.Context, tenancy.TenantScope) ([]KeyResource, error)
	revealKey          func(context.Context, tenancy.TenantScope, string) (string, error)
	discoverMCPTools   func(context.Context, tenancy.TenantScope, string) ([]DiscoveredTool, error)
	createCredential   func(context.Context, tenancy.TenantScope, CredentialCreateInput, string) error
	rotateCredential   func(context.Context, tenancy.TenantScope, string, []byte, string) error
	disableCredential  func(context.Context, tenancy.TenantScope, string, string) error
	deleteCredential   func(context.Context, tenancy.TenantScope, string, string) error
	playground         func(context.Context, tenancy.TenantScope, PlaygroundRequest) (PlaygroundResponse, error)
	playgroundStream   func(context.Context, tenancy.TenantScope, PlaygroundRequest, func(StreamEventView)) (PlaygroundResponse, error)
	createProject      func(context.Context, tenancy.TenantScope, ProjectCreateInput) (ProjectSummary, error)
	listProjects       func(context.Context, tenancy.TenantScope) ([]ProjectSummary, error)
	listTenants        func(context.Context) ([]TenantSummary, error)
	createTenant       func(context.Context, TenantCreateInput, string) (TenantSummary, error)
	updateTenantStatus func(context.Context, string, TenantStatusInput, string) (TenantSummary, error)
	auditRetention     func(context.Context, tenancy.TenantScope) (AuditRetentionView, error)
	setAuditRetention  func(context.Context, tenancy.TenantScope, AuditRetentionInput, string) (AuditRetentionView, error)
	listAudit          func(context.Context, tenancy.TenantScope, int, int) ([]AuditRecord, error)
	// healthView replaces the config-derived health view (the Lite profile
	// real-probes upstream providers).
	healthView func(context.Context, tenancy.TenantScope) (HealthView, error)
	// federationOverlay post-processes the snapshot-derived federation view
	// (the Lite profile overlays live lifecycle state).
	federationOverlay func(context.Context, tenancy.TenantScope, FederationView) (FederationView, error)
	// evidenceExporter assembles the tenant evidence archive on demand; nil
	// makes the Evidence capability fail closed (no export).
	evidenceExporter func(context.Context, tenancy.TenantScope, time.Time, time.Time) (guardrailbench.Archive, error)
}

type ResourceCatalogStore interface {
	Catalog(context.Context) (catalog.ResourceCatalogView, error)
}

func NewControlBackend(
	configService *config.Service,
	accountingRepo accounting.Repository,
	resourceCatalog ResourceCatalogStore,
	alertService *alert.Service,
	alertStore alert.Store,
	registry *runtime.ActiveRegistry,
	circuits CircuitViewer,
	live *LiveBus,
	securityEvents func(context.Context, tenancy.TenantScope, int) ([]SecurityEventView, error),
	toolCalls func(context.Context, tenancy.TenantScope, int) ([]ToolCallView, error),
) *ControlBackend {
	notificationStore, _ := alertStore.(alert.NotificationSettingsStore)
	return &ControlBackend{
		configService: configService, accounting: accountingRepo, resourceCatalog: resourceCatalog, alertService: alertService,
		alertStore: alertStore, notificationStore: notificationStore, registry: registry, circuits: circuits, live: live,
		guardrails: guardraildomain.NewPolicyRegistry(nil), securityEvents: securityEvents, toolCalls: toolCalls,
	}
}

// SetGuardrailRegistry replaces the default in-memory-only guardrail registry
// with one backed by a persistent store and audit recorder. Call it at
// composition time, before the server starts serving.
func (b *ControlBackend) SetGuardrailRegistry(registry *guardraildomain.PolicyRegistry) {
	b.guardrails = registry
}

func (b *ControlBackend) SetNotificationReloader(reload func(context.Context, alert.NotificationSettings) error) {
	b.notificationReload = reload
}

func (b *ControlBackend) FastPublishGuardrail(ctx context.Context, scope tenancy.TenantScope, policy guardraildomain.Policy, change guardraildomain.ChangeType, actor string) (guardraildomain.Policy, error) {
	published, err := b.guardrails.FastPublish(ctx, scope, policy, change, actor)
	if err != nil {
		return guardraildomain.Policy{}, err
	}
	// A fast publish is only real once the data plane enforces it: overlay the
	// policy onto the tenant's active runtime snapshot.
	if b.configService == nil {
		return published, nil
	}
	mode := string(change)
	if snapshot := b.snapshotByTenant(ctx, scope); snapshot != nil && snapshot.GuardrailPolicy().Mode != "" {
		mode = snapshot.GuardrailPolicy().Mode
	}
	rules := make([]runtime.GuardrailRule, 0, len(published.Rules))
	for _, rule := range published.Rules {
		rules = append(rules, runtime.GuardrailRule{ID: rule.ID, Kind: rule.Kind, Pattern: rule.Pattern, Action: rule.Action, Replacement: rule.Replacement})
	}
	if err := b.configService.OverlayGuardrail(ctx, scope, runtime.GuardrailPolicy{Mode: mode, SecurityEpoch: published.SecurityEpoch, Rules: rules}); err != nil {
		return guardraildomain.Policy{}, err
	}
	return published, nil
}

func (b *ControlBackend) SecurityEvents(ctx context.Context, scope tenancy.TenantScope, limit int) ([]SecurityEventView, error) {
	if b.securityEvents == nil {
		return nil, errNotWired
	}
	return b.securityEvents(ctx, scope, limit)
}

func (b *ControlBackend) ToolCalls(ctx context.Context, scope tenancy.TenantScope, limit int) ([]ToolCallView, error) {
	if b.toolCalls == nil {
		return nil, errNotWired
	}
	return b.toolCalls(ctx, scope, limit)
}

func (b *ControlBackend) Approvals(ctx context.Context, scope tenancy.TenantScope, limit int) ([]ApprovalView, error) {
	if b.approvals == nil {
		return nil, errNotWired
	}
	return b.approvals(ctx, scope, limit)
}

// SetApprovalLister wires the approval inbox read path.
func (b *ControlBackend) SetApprovalLister(fn func(context.Context, tenancy.TenantScope, int) ([]ApprovalView, error)) {
	b.approvals = fn
}

func (b *ControlBackend) ApprovalAction(ctx context.Context, scope tenancy.TenantScope, id, decision, actor string) error {
	if b.approvalAction == nil {
		return errNotWired
	}
	return b.approvalAction(ctx, scope, id, decision, actor)
}

// SetApprovalAction wires the approval decide path.
func (b *ControlBackend) SetApprovalAction(fn func(context.Context, tenancy.TenantScope, string, string, string) error) {
	b.approvalAction = fn
}

func (b *ControlBackend) FederationSuspend(ctx context.Context, scope tenancy.TenantScope, id, actor string) error {
	if b.federationSuspend == nil {
		return errNotWired
	}
	return b.federationSuspend(ctx, scope, id, actor)
}

// SetFederationSuspend wires the federated relationship suspend path.
func (b *ControlBackend) SetFederationSuspend(fn func(context.Context, tenancy.TenantScope, string, string) error) {
	b.federationSuspend = fn
}

func (b *ControlBackend) AgentGraph(ctx context.Context, scope tenancy.TenantScope, rootTask string) (AgentGraphView, error) {
	if b.agentGraph == nil {
		return AgentGraphView{}, errNotWired
	}
	return b.agentGraph(ctx, scope, rootTask)
}

// SetAgentGraph wires the agent/task graph read path.
func (b *ControlBackend) SetAgentGraph(fn func(context.Context, tenancy.TenantScope, string) (AgentGraphView, error)) {
	b.agentGraph = fn
}

func (b *ControlBackend) Federation(ctx context.Context, scope tenancy.TenantScope) (FederationView, error) {
	if b.registry == nil {
		return FederationView{}, errNotWired
	}
	snapshot := b.snapshotByTenant(ctx, scope)
	if snapshot == nil {
		return FederationView{}, nil
	}
	view := FederationView{}
	for _, relationship := range snapshot.FederationRelationships() {
		view.Relationships = append(view.Relationships, RelationshipView{
			ID: relationship.ID, ExternalAgentID: relationship.ExternalAgentID, Status: relationship.Status,
			AssuranceLevel: relationship.AssuranceLevel, HasVerifiedAnchor: relationship.HasVerifiedAnchor,
		})
	}
	for _, agent := range snapshot.FederatedAgents() {
		view.ExternalAgents = append(view.ExternalAgents, ExternalAgentView{ID: agent.ID, Name: agent.Name, ExternalSubject: agent.ExternalSubject, TrustBoundary: agent.TrustBoundary, Status: agent.Status})
	}
	if b.federationOverlay != nil {
		return b.federationOverlay(ctx, scope, view)
	}
	return view, nil
}

// SetFederationOverlay wires the post-processing step for the snapshot-derived
// federation view (the Lite profile overlays live lifecycle state).
func (b *ControlBackend) SetFederationOverlay(fn func(context.Context, tenancy.TenantScope, FederationView) (FederationView, error)) {
	b.federationOverlay = fn
}

// FederationDiscover wires the Agent Card discovery→candidate path: it fetches
// and verifies a peer Agent Card and creates (or re-reviews) the federation
// relationship candidate. Discovery never implies trust.
func (b *ControlBackend) FederationDiscover(ctx context.Context, scope tenancy.TenantScope, input FederationDiscoverInput) (RelationshipView, error) {
	if b.federationDiscover == nil {
		return RelationshipView{}, errNotWired
	}
	return b.federationDiscover(ctx, scope, input)
}

// SetFederationDiscover wires the Agent Card discovery path.
func (b *ControlBackend) SetFederationDiscover(fn func(context.Context, tenancy.TenantScope, FederationDiscoverInput) (RelationshipView, error)) {
	b.federationDiscover = fn
}

// FederationReview reviews a discovered relationship. Approval requires a
// verified anchor plus project and capability grants before it can activate.
func (b *ControlBackend) FederationReview(ctx context.Context, scope tenancy.TenantScope, id string, input FederationReviewInput, actor string) error {
	if b.federationReview == nil {
		return errNotWired
	}
	return b.federationReview(ctx, scope, id, input, actor)
}

// SetFederationReview wires the relationship review/activation path.
func (b *ControlBackend) SetFederationReview(fn func(context.Context, tenancy.TenantScope, string, FederationReviewInput, string) error) {
	b.federationReview = fn
}

// PushDeliveries lists the tenant's most recent sanitized A2A push callback
// deliveries, optionally filtered by status.
func (b *ControlBackend) PushDeliveries(ctx context.Context, scope tenancy.TenantScope, status string, limit int) ([]PushDeliveryView, error) {
	if b.pushDeliveries == nil {
		return nil, errNotWired
	}
	return b.pushDeliveries(ctx, scope, status, limit)
}

func (b *ControlBackend) Delegations(ctx context.Context, scope tenancy.TenantScope) ([]DelegationGrantView, error) {
	if b.delegations == nil {
		return nil, errNotWired
	}
	return b.delegations(ctx, scope)
}

func (b *ControlBackend) GrantDelegation(ctx context.Context, scope tenancy.TenantScope, input DelegationGrantInput, actor string) (DelegationGrantView, error) {
	if b.grantDelegation == nil {
		return DelegationGrantView{}, errNotWired
	}
	return b.grantDelegation(ctx, scope, input, actor)
}

func (b *ControlBackend) RevokeDelegation(ctx context.Context, scope tenancy.TenantScope, id, actor string) error {
	if b.revokeDelegation == nil {
		return errNotWired
	}
	return b.revokeDelegation(ctx, scope, id, actor)
}

func (b *ControlBackend) SetDelegations(fn func(context.Context, tenancy.TenantScope) ([]DelegationGrantView, error)) {
	b.delegations = fn
}

func (b *ControlBackend) SetGrantDelegation(fn func(context.Context, tenancy.TenantScope, DelegationGrantInput, string) (DelegationGrantView, error)) {
	b.grantDelegation = fn
}

func (b *ControlBackend) SetRevokeDelegation(fn func(context.Context, tenancy.TenantScope, string, string) error) {
	b.revokeDelegation = fn
}

// SetPushDeliveries wires the delivery drill-down read path.
func (b *ControlBackend) SetPushDeliveries(fn func(context.Context, tenancy.TenantScope, string, int) ([]PushDeliveryView, error)) {
	b.pushDeliveries = fn
}

// SetEvidenceExporter wires the Evidence Export capability. The fn assembles
// the read-only tenant archive; a nil fn makes Evidence fail closed.
func (b *ControlBackend) SetEvidenceExporter(fn func(context.Context, tenancy.TenantScope, time.Time, time.Time) (guardrailbench.Archive, error)) {
	b.evidenceExporter = fn
}

// Evidence exports the tenant evidence archive for the requested time range.
func (b *ControlBackend) Evidence(ctx context.Context, scope tenancy.TenantScope, from, to time.Time) (guardrailbench.Archive, error) {
	if b.evidenceExporter == nil {
		return guardrailbench.Archive{}, errors.New("evidence export not wired")
	}
	return b.evidenceExporter(ctx, scope, from, to)
}

// --- Capability slots for the runnable Lite paths ---------------------------
// Each method delegates to its Set* slot; an unwired slot answers
// errNotWired so the server maps it to 501 NOT_IMPLEMENTED.

func (b *ControlBackend) Setup(ctx context.Context, raw json.RawMessage) (WizardSetupResponse, error) {
	if b.setupWizard == nil {
		return WizardSetupResponse{}, errNotWired
	}
	return b.setupWizard(ctx, raw)
}

// SetSetupWizard wires the one-time tenant setup wizard.
func (b *ControlBackend) SetSetupWizard(fn func(context.Context, json.RawMessage) (WizardSetupResponse, error)) {
	b.setupWizard = fn
}

func (b *ControlBackend) CreateKey(ctx context.Context, scope tenancy.TenantScope, input apikey.CreateInput, actorID string) (*apikey.CreateResult, error) {
	if b.createKey == nil {
		return nil, errNotWired
	}
	return b.createKey(ctx, scope, input, actorID)
}

func (b *ControlBackend) Tenants(ctx context.Context) ([]TenantSummary, error) {
	if b.listTenants == nil {
		return nil, errNotWired
	}
	return b.listTenants(ctx)
}

func (b *ControlBackend) CreateTenant(ctx context.Context, input TenantCreateInput, actorID string) (TenantSummary, error) {
	if b.createTenant == nil {
		return TenantSummary{}, errNotWired
	}
	return b.createTenant(ctx, input, actorID)
}

func (b *ControlBackend) UpdateTenantStatus(ctx context.Context, id string, input TenantStatusInput, actorID string) (TenantSummary, error) {
	if b.updateTenantStatus == nil {
		return TenantSummary{}, errNotWired
	}
	return b.updateTenantStatus(ctx, id, input, actorID)
}

func (b *ControlBackend) SetTenants(fn func(context.Context) ([]TenantSummary, error)) {
	b.listTenants = fn
}

func (b *ControlBackend) SetCreateTenant(fn func(context.Context, TenantCreateInput, string) (TenantSummary, error)) {
	b.createTenant = fn
}

func (b *ControlBackend) SetUpdateTenantStatus(fn func(context.Context, string, TenantStatusInput, string) (TenantSummary, error)) {
	b.updateTenantStatus = fn
}

func (b *ControlBackend) GetAuditRetention(ctx context.Context, scope tenancy.TenantScope) (AuditRetentionView, error) {
	if b.auditRetention == nil {
		return AuditRetentionView{}, errNotWired
	}
	return b.auditRetention(ctx, scope)
}

func (b *ControlBackend) SetAuditRetention(ctx context.Context, scope tenancy.TenantScope, input AuditRetentionInput, actorID string) (AuditRetentionView, error) {
	if b.setAuditRetention == nil {
		return AuditRetentionView{}, errNotWired
	}
	return b.setAuditRetention(ctx, scope, input, actorID)
}

func (b *ControlBackend) SetAuditRetentionRead(fn func(context.Context, tenancy.TenantScope) (AuditRetentionView, error)) {
	b.auditRetention = fn
}

func (b *ControlBackend) SetAuditRetentionWrite(fn func(context.Context, tenancy.TenantScope, AuditRetentionInput, string) (AuditRetentionView, error)) {
	b.setAuditRetention = fn
}

// SetCreateKey wires the API key create path (with runtime re-activation).
func (b *ControlBackend) SetCreateKey(fn func(context.Context, tenancy.TenantScope, apikey.CreateInput, string) (*apikey.CreateResult, error)) {
	b.createKey = fn
}

func (b *ControlBackend) RevokeKey(ctx context.Context, scope tenancy.TenantScope, id string, actorID string) error {
	if b.revokeKey == nil {
		return errNotWired
	}
	return b.revokeKey(ctx, scope, id, actorID)
}

// SetRevokeKey wires the API key revoke path (with runtime re-activation).
func (b *ControlBackend) SetRevokeKey(fn func(context.Context, tenancy.TenantScope, string, string) error) {
	b.revokeKey = fn
}

func (b *ControlBackend) ListKeys(ctx context.Context, scope tenancy.TenantScope) ([]KeyResource, error) {
	if b.listKeys == nil {
		return nil, errNotWired
	}
	return b.listKeys(ctx, scope)
}

// SetListKeys wires the API key list path.
func (b *ControlBackend) SetListKeys(fn func(context.Context, tenancy.TenantScope) ([]KeyResource, error)) {
	b.listKeys = fn
}

func (b *ControlBackend) RevealKey(ctx context.Context, scope tenancy.TenantScope, id string) (string, error) {
	if b.revealKey == nil {
		return "", errNotWired
	}
	return b.revealKey(ctx, scope, id)
}

// SetRevealKey wires the one-time API key reveal path.
func (b *ControlBackend) SetRevealKey(fn func(context.Context, tenancy.TenantScope, string) (string, error)) {
	b.revealKey = fn
}

func (b *ControlBackend) DiscoverMCPTools(ctx context.Context, scope tenancy.TenantScope, id string) ([]DiscoveredTool, error) {
	if b.discoverMCPTools == nil {
		return nil, errNotWired
	}
	return b.discoverMCPTools(ctx, scope, id)
}

// SetDiscoverMCPTools wires the live MCP tool discovery path.
func (b *ControlBackend) SetDiscoverMCPTools(fn func(context.Context, tenancy.TenantScope, string) ([]DiscoveredTool, error)) {
	b.discoverMCPTools = fn
}

func (b *ControlBackend) CreateCredential(ctx context.Context, scope tenancy.TenantScope, input CredentialCreateInput, actor string) error {
	if b.createCredential == nil {
		return errNotWired
	}
	return b.createCredential(ctx, scope, input, actor)
}

// SetCreateCredential wires the provider credential create path.
func (b *ControlBackend) SetCreateCredential(fn func(context.Context, tenancy.TenantScope, CredentialCreateInput, string) error) {
	b.createCredential = fn
}

func (b *ControlBackend) RotateCredential(ctx context.Context, scope tenancy.TenantScope, id string, secret []byte, actor string) error {
	if b.rotateCredential == nil {
		return errNotWired
	}
	return b.rotateCredential(ctx, scope, id, secret, actor)
}

// SetRotateCredential wires the provider credential rotate path.
func (b *ControlBackend) SetRotateCredential(fn func(context.Context, tenancy.TenantScope, string, []byte, string) error) {
	b.rotateCredential = fn
}

func (b *ControlBackend) DisableCredential(ctx context.Context, scope tenancy.TenantScope, id, actor string) error {
	if b.disableCredential == nil {
		return errNotWired
	}
	return b.disableCredential(ctx, scope, id, actor)
}

// SetDisableCredential wires the provider credential disable path.
func (b *ControlBackend) SetDisableCredential(fn func(context.Context, tenancy.TenantScope, string, string) error) {
	b.disableCredential = fn
}

func (b *ControlBackend) DeleteCredential(ctx context.Context, scope tenancy.TenantScope, id, actor string) error {
	if b.deleteCredential == nil {
		return errNotWired
	}
	return b.deleteCredential(ctx, scope, id, actor)
}

// SetDeleteCredential wires the provider credential delete path.
func (b *ControlBackend) SetDeleteCredential(fn func(context.Context, tenancy.TenantScope, string, string) error) {
	b.deleteCredential = fn
}

func (b *ControlBackend) Playground(ctx context.Context, scope tenancy.TenantScope, input PlaygroundRequest) (PlaygroundResponse, error) {
	if b.playground == nil {
		return PlaygroundResponse{}, errNotWired
	}
	return b.playground(ctx, scope, input)
}

// SetPlayground wires the governed playground execution path.
func (b *ControlBackend) SetPlayground(fn func(context.Context, tenancy.TenantScope, PlaygroundRequest) (PlaygroundResponse, error)) {
	b.playground = fn
}

func (b *ControlBackend) PlaygroundStream(ctx context.Context, scope tenancy.TenantScope, input PlaygroundRequest, emit func(StreamEventView)) (PlaygroundResponse, error) {
	if b.playgroundStream == nil {
		return PlaygroundResponse{}, errNotWired
	}
	return b.playgroundStream(ctx, scope, input, emit)
}

// SetPlaygroundStream wires the streaming playground path.
func (b *ControlBackend) SetPlaygroundStream(fn func(context.Context, tenancy.TenantScope, PlaygroundRequest, func(StreamEventView)) (PlaygroundResponse, error)) {
	b.playgroundStream = fn
}

func (b *ControlBackend) CreateProject(ctx context.Context, scope tenancy.TenantScope, input ProjectCreateInput) (ProjectSummary, error) {
	if b.createProject == nil {
		return ProjectSummary{}, errNotWired
	}
	return b.createProject(ctx, scope, input)
}

// SetCreateProject wires the tenant project create path.
func (b *ControlBackend) SetCreateProject(fn func(context.Context, tenancy.TenantScope, ProjectCreateInput) (ProjectSummary, error)) {
	b.createProject = fn
}

func (b *ControlBackend) Audit(ctx context.Context, scope tenancy.TenantScope, limit, offset int) ([]AuditRecord, error) {
	if b.listAudit == nil {
		return nil, errNotWired
	}
	return b.listAudit(ctx, scope, limit, offset)
}

// SetAuditLister wires the tenant audit trail read path.
func (b *ControlBackend) SetAuditLister(fn func(context.Context, tenancy.TenantScope, int, int) ([]AuditRecord, error)) {
	b.listAudit = fn
}

// SetHealthView replaces the config-derived health view with a real upstream
// prober (the Lite profile).
func (b *ControlBackend) SetHealthView(fn func(context.Context, tenancy.TenantScope) (HealthView, error)) {
	b.healthView = fn
}

func (b *ControlBackend) Simulate(ctx context.Context, scope tenancy.TenantScope, input SimulateRequest) (SimulateResult, error) {
	if b.registry == nil {
		return SimulateResult{}, errNotWired
	}
	snapshot := b.snapshotByTenant(ctx, scope)
	if snapshot == nil {
		return SimulateResult{}, nil
	}
	metrics := b.recentDeploymentMetrics(ctx, scope)
	for id, metric := range input.Metrics {
		metrics[id] = routing.DeploymentMetrics{LatencyMS: metric.LatencyMS, Cost: metric.Cost, Load: metric.Load, CacheAffinity: metric.CacheAffinity}
	}
	plan, err := (&routing.Planner{}).Plan(routing.Input{Snapshot: snapshot, LogicalModel: input.Model, ProjectID: input.ProjectID, RequestID: "simulator", Metrics: metrics})
	if err != nil {
		return SimulateResult{}, err
	}
	result := SimulateResult{Selected: plan.Selected(), Fallback: plan.Fallback()}
	for _, evidence := range plan.Evidence() {
		result.Evidence = append(result.Evidence, SimulateEvidence{DeploymentID: evidence.DeploymentID, Eligible: evidence.Eligible, Score: evidence.SelectionScore, Breakdown: evidence.ScoreBreakdown})
	}
	return result, nil
}

func (b *ControlBackend) recentDeploymentMetrics(ctx context.Context, scope tenancy.TenantScope) map[string]routing.DeploymentMetrics {
	metrics := map[string]routing.DeploymentMetrics{}
	if b.accounting == nil {
		return metrics
	}
	requests, err := b.accounting.ListRequests(ctx, scope, 500)
	if err != nil {
		return metrics
	}
	type accumulator struct {
		latency int64
		cost    float64
		calls   int64
		cache   int64
	}
	acc := map[string]*accumulator{}
	for _, request := range requests {
		if request.DeploymentID == "" {
			continue
		}
		item := acc[request.DeploymentID]
		if item == nil {
			item = &accumulator{}
			acc[request.DeploymentID] = item
		}
		item.calls++
		item.latency += request.LatencyMS
		if request.ProviderCost != nil {
			item.cost += *request.ProviderCost
		}
		if request.Source == "cache" || request.Source == "semantic_cache" {
			item.cache++
		}
	}
	for deploymentID, item := range acc {
		if item.calls == 0 {
			continue
		}
		metrics[deploymentID] = routing.DeploymentMetrics{
			LatencyMS:     float64(item.latency) / float64(item.calls),
			Cost:          item.cost / float64(item.calls),
			Load:          float64(item.calls),
			CacheAffinity: float64(item.cache) / float64(item.calls),
		}
	}
	return metrics
}

var errNotWired = errors.New("operation not wired in this composition")

// ErrRuntimeRefreshFailed reports that a tenant mutation (API key create or
// revoke) was persisted but its re-activation on the data plane snapshot
// failed. The change is durable and takes effect on the next successful
// reconcile, publish, or restart.
var ErrRuntimeRefreshFailed = errors.New("runtime refresh failed")

func (b *ControlBackend) Projects(ctx context.Context, scope tenancy.TenantScope) ([]ProjectSummary, error) {
	if b.listProjects != nil {
		return b.listProjects(ctx, scope)
	}
	if b.registry == nil {
		return nil, errNotWired
	}
	return []ProjectSummary{}, nil
}

// SetProjectsLister replaces the placeholder project list with a real
// repository-backed lister (the Lite profile).
func (b *ControlBackend) SetProjectsLister(fn func(context.Context, tenancy.TenantScope) ([]ProjectSummary, error)) {
	b.listProjects = fn
}

// Dashboard aggregates request/token/latency metrics server-side (§37.49/50).
func (b *ControlBackend) Dashboard(ctx context.Context, scope tenancy.TenantScope) (DashboardView, error) {
	if b.accounting == nil {
		return DashboardView{}, errNotWired
	}
	requests, err := b.accounting.ListRequests(ctx, scope, 500)
	if err != nil {
		return DashboardView{}, err
	}
	view := DashboardView{ConfigVersion: b.activeConfigVersion(ctx, scope)}
	for _, request := range requests {
		view.Requests++
		view.InputTokens += request.InputTokens
		view.OutputTokens += request.OutputTokens
		view.AvgLatencyMS += request.LatencyMS
		if request.ProviderCost != nil {
			view.Spend += *request.ProviderCost
		}
	}
	if view.Requests > 0 {
		view.AvgLatencyMS /= view.Requests
	}
	return view, nil
}

func (b *ControlBackend) FinOps(ctx context.Context, scope tenancy.TenantScope) (FinOpsView, error) {
	if b.accounting == nil {
		return FinOpsView{}, errNotWired
	}
	requests, err := b.accounting.ListRequests(ctx, scope, 500)
	if err != nil {
		return FinOpsView{}, err
	}
	view := FinOpsView{EstimateVersion: pricing.LiteReferencePriceVersion.ID}
	projects := map[string]*FinOpsProjectRow{}
	models := map[string]*FinOpsModelRow{}
	var projectOrder, modelOrder []string
	for _, request := range requests {
		view.Requests++
		view.InputTokens += request.InputTokens
		view.OutputTokens += request.OutputTokens
		cost := 0.0
		if request.ProviderCost != nil {
			cost = *request.ProviderCost
			view.Spend += cost
		}
		if request.Source == "cache" {
			view.CacheHits++
		}
		if request.Source == "semantic_cache" {
			view.SemanticHits++
		}
		// A cache hit avoided the full would-be provider token load. Dollar
		// savings are reported only when the historical record resolves
		// unambiguously to one reference-priced upstream model.
		savedTokens := int64(0)
		cacheSavings := 0.0
		pricedSavedTokens := int64(0)
		if request.Source == "cache" || request.Source == "semantic_cache" {
			savedTokens = request.InputTokens + request.OutputTokens
			view.SavedTokens += savedTokens
			if estimate, ok := b.cacheSavingsEstimate(scope, request); ok {
				cacheSavings = estimate
				pricedSavedTokens = savedTokens
				view.CacheSavings += estimate
				view.PricedSavedTokens += savedTokens
			} else {
				view.UnpricedSavedTokens += savedTokens
			}
		}
		optimization := aggregate.QuantifyOptimizationCost(request, 0, 0)
		view.RetryCost += optimization.RetryCost
		view.FallbackCost += optimization.FallbackCost
		projectID := request.ProjectID
		if projectID == "" {
			projectID = "unattributed"
		}
		project, ok := projects[projectID]
		if !ok {
			project = &FinOpsProjectRow{ProjectID: projectID}
			projects[projectID] = project
			projectOrder = append(projectOrder, projectID)
		}
		project.Requests++
		project.InputTokens += request.InputTokens
		project.OutputTokens += request.OutputTokens
		project.Spend += cost
		if request.Source == "cache" {
			project.CacheHits++
		}
		if request.Source == "semantic_cache" {
			project.SemanticHits++
		}
		project.SavedTokens += savedTokens
		project.PricedSavedTokens += pricedSavedTokens
		project.UnpricedSavedTokens += savedTokens - pricedSavedTokens
		project.CacheSavings += cacheSavings
		modelID := request.LogicalModel
		if modelID == "" {
			modelID = "unknown"
		}
		model, ok := models[modelID]
		if !ok {
			model = &FinOpsModelRow{LogicalModel: modelID}
			models[modelID] = model
			modelOrder = append(modelOrder, modelID)
		}
		model.Requests++
		model.InputTokens += request.InputTokens
		model.OutputTokens += request.OutputTokens
		model.Spend += cost
		model.SavedTokens += savedTokens
		model.PricedSavedTokens += pricedSavedTokens
		model.UnpricedSavedTokens += savedTokens - pricedSavedTokens
		model.CacheSavings += cacheSavings
		if request.Source == "cache" {
			model.CacheHits++
		}
		if request.Source == "semantic_cache" {
			model.SemanticHits++
		}
		model.RetryCount += request.RetryCount
		model.FallbackCount += request.FallbackCount
	}
	for _, id := range projectOrder {
		projects[id].CacheHitRate = float64(projects[id].CacheHits+projects[id].SemanticHits) / float64(projects[id].Requests)
		view.ByProject = append(view.ByProject, *projects[id])
	}
	for _, id := range modelOrder {
		models[id].CacheHitRate = float64(models[id].CacheHits+models[id].SemanticHits) / float64(models[id].Requests)
		view.ByModel = append(view.ByModel, *models[id])
	}
	if view.Requests > 0 {
		view.CacheHitRate = float64(view.CacheHits+view.SemanticHits) / float64(view.Requests)
		view.SemanticHitRate = float64(view.SemanticHits) / float64(view.Requests)
	}
	sort.SliceStable(view.ByProject, func(i, j int) bool { return view.ByProject[i].Spend > view.ByProject[j].Spend })
	sort.SliceStable(view.ByModel, func(i, j int) bool { return view.ByModel[i].Spend > view.ByModel[j].Spend })
	view.Recommendations = finopsRecommendations(view)
	return view, nil
}

func (b *ControlBackend) cacheSavingsEstimate(scope tenancy.TenantScope, request accounting.RequestRecord) (float64, bool) {
	if b.registry == nil {
		return 0, false
	}
	snapshot, ok := b.registry.TenantByID(scope.TenantID)
	if !ok || request.SnapshotVersion > 0 && request.SnapshotVersion != snapshot.Version {
		return 0, false
	}
	upstreamModel := ""
	if request.DeploymentID != "" {
		deployment, exists := snapshot.Deployment(request.DeploymentID)
		if !exists {
			return 0, false
		}
		upstreamModel = deployment.UpstreamModel
	} else {
		logical, exists := snapshot.LogicalModel(request.LogicalModel)
		if !exists {
			return 0, false
		}
		route, exists := snapshot.RoutePolicy(logical.RoutePolicyID)
		if !exists {
			return 0, false
		}
		for _, deploymentID := range route.DeploymentIDs {
			deployment, exists := snapshot.Deployment(deploymentID)
			if !exists || deployment.Status != "enabled" || deployment.UpstreamModel == "" {
				continue
			}
			if upstreamModel != "" && upstreamModel != deployment.UpstreamModel {
				return 0, false
			}
			upstreamModel = deployment.UpstreamModel
		}
	}
	if upstreamModel == "" {
		return 0, false
	}
	estimate, err := pricing.Price(pricing.LiteReferencePriceVersion, upstreamModel, "USD", request.InputTokens, request.OutputTokens)
	return estimate, err == nil
}

func (b *ControlBackend) Recommendations(ctx context.Context, scope tenancy.TenantScope) ([]RecommendationView, error) {
	view, err := b.FinOps(ctx, scope)
	if err != nil {
		return nil, err
	}
	window := controlrecommend.EvidenceWindow{From: time.Now().UTC().Add(-time.Hour), To: time.Now().UTC(), Samples: view.Requests, Metric: "provider_cost"}
	service := controlrecommend.NewService(time.Now)
	var result []RecommendationView
	if rec := service.CostThresholdRecommendation(scope.TenantID, "provider_cost", view.Spend, 0, window); rec != nil && rec.Valid() {
		result = append(result, RecommendationView{
			ID: rec.ID, Kind: rec.Kind, Title: rec.Title, Explanation: rec.Explanation,
			Change: rec.Change, Metric: rec.Evidence.Metric, Samples: rec.Evidence.Samples,
			From: rec.Evidence.From, To: rec.Evidence.To, CreatedAt: rec.CreatedAt, Acceptable: rec.Acceptable,
		})
	}
	return result, nil
}

func finopsRecommendations(view FinOpsView) []FinOpsRecommendation {
	var items []FinOpsRecommendation
	if view.RetryCost+view.FallbackCost > 0 {
		items = append(items, FinOpsRecommendation{Kind: "routing", Title: "Review retry and fallback costs", Detail: "Retries or fallback attempts are adding provider spend. Check provider health and route ordering.", Impact: view.RetryCost + view.FallbackCost})
	}
	if view.CacheHits+view.SemanticHits > 0 {
		items = append(items, FinOpsRecommendation{Kind: "cache", Title: "Cache is avoiding duplicate provider calls", Detail: fmt.Sprintf("Cache hits avoided ~%d provider tokens; %d were priced at %s reference rates for an estimated %s avoided cost (%d unpriced). This is not provider billing.", view.SavedTokens, view.PricedSavedTokens, view.EstimateVersion, moneyText(view.CacheSavings), view.UnpricedSavedTokens), Impact: view.CacheSavings})
	}
	if len(view.ByProject) > 0 && view.ByProject[0].Spend > 0 {
		items = append(items, FinOpsRecommendation{Kind: "attribution", Title: "Top project drives current spend", Detail: fmt.Sprintf("Project %s accounts for %.4f provider spend in the recent ledger window.", view.ByProject[0].ProjectID, view.ByProject[0].Spend), Impact: view.ByProject[0].Spend})
	}
	if len(items) == 0 {
		items = append(items, FinOpsRecommendation{Kind: "baseline", Title: "No optimization signals yet", Detail: "Recent usage has no retry/fallback overhead or cache activity to act on."})
	}
	return items
}

func (b *ControlBackend) Drafts(ctx context.Context, scope tenancy.TenantScope) ([]DraftSummary, error) {
	if b.configService == nil {
		return nil, errNotWired
	}
	drafts, err := b.configService.Drafts(ctx, scope)
	if err != nil {
		return nil, err
	}
	// Return a summary only: never echo the full draft config (secrets) to the
	// list surface. The editor loads one draft via GET /drafts/{id}.
	result := make([]DraftSummary, 0, len(drafts))
	for _, draft := range drafts {
		result = append(result, DraftSummary{
			ID: draft.ID, Status: draft.Status, BaseVersion: draft.BaseVersion,
			Revision: draft.Revision, UpdatedAt: draft.UpdatedAt,
		})
	}
	return result, nil
}

func (b *ControlBackend) activeConfigVersion(ctx context.Context, scope tenancy.TenantScope) int64 {
	if b.registry == nil {
		return 0
	}
	if snapshot, ok := b.registry.TenantByID(scope.TenantID); ok {
		return snapshot.Version
	}
	return 0
}

func (b *ControlBackend) Runtime(ctx context.Context, scope tenancy.TenantScope) (RuntimeResources, error) {
	if b.registry == nil {
		return RuntimeResources{}, errNotWired
	}
	snapshot := b.snapshotByTenant(ctx, scope)
	if snapshot == nil {
		return RuntimeResources{}, nil
	}
	return buildRuntimeResources(snapshot), nil
}

func (b *ControlBackend) ResourceCatalog(ctx context.Context, _ tenancy.TenantScope) (ResourceCatalogView, error) {
	if b.resourceCatalog == nil {
		return ResourceCatalogView{}, errNotWired
	}
	return b.resourceCatalog.Catalog(ctx)
}

func (b *ControlBackend) GetDraft(ctx context.Context, scope tenancy.TenantScope, id string) (*config.Draft, error) {
	return b.configService.Draft(ctx, scope, id)
}

func (b *ControlBackend) CreateDraft(ctx context.Context, scope tenancy.TenantScope, actorID string) (*config.Draft, error) {
	if b.configService == nil {
		return nil, errNotWired
	}
	return b.configService.CreateDraft(ctx, scope, actorID)
}

func (b *ControlBackend) UpdateDraft(ctx context.Context, scope tenancy.TenantScope, id string, revision int64, document config.TenantConfig, actorID string) (*config.Draft, error) {
	return b.configService.UpdateDraft(ctx, scope, id, revision, document, actorID)
}

func (b *ControlBackend) Diff(ctx context.Context, scope tenancy.TenantScope, id string) ([]config.DiffEntry, error) {
	return b.configService.Diff(ctx, scope, id)
}

func (b *ControlBackend) Publish(ctx context.Context, scope tenancy.TenantScope, id string, revision int64, actorID string) (*config.Version, []config.Diagnostic, error) {
	return b.configService.Publish(ctx, scope, id, revision, actorID)
}

func (b *ControlBackend) Rollback(ctx context.Context, scope tenancy.TenantScope, version int64, actorID string) (*config.Version, error) {
	return b.configService.Rollback(ctx, scope, version, actorID)
}

func (b *ControlBackend) Versions(ctx context.Context, scope tenancy.TenantScope) ([]config.Version, error) {
	if b.configService == nil {
		return nil, errNotWired
	}
	return b.configService.Versions(ctx, scope)
}

// SystemConfig returns the persisted singleton system config. A composition
// without a system repository answers errNotWired (HTTP 501).
func (b *ControlBackend) SystemConfig(ctx context.Context) (config.SystemConfig, error) {
	if b.configService == nil {
		return config.SystemConfig{}, errNotWired
	}
	return b.configService.SystemConfig(ctx)
}

// SetSystemConfig upserts the singleton system config on behalf of the acting
// system admin. The presentation layer gates the route.
func (b *ControlBackend) SetSystemConfig(ctx context.Context, document config.SystemConfig, actorID string) (config.SystemConfig, error) {
	if b.configService == nil {
		return config.SystemConfig{}, errNotWired
	}
	return b.configService.SetSystemConfig(ctx, document, actorID)
}

func (b *ControlBackend) Request(ctx context.Context, scope tenancy.TenantScope, id string) (*accounting.RequestRecord, error) {
	return b.accounting.GetRequest(ctx, scope, id)
}

func (b *ControlBackend) Requests(ctx context.Context, scope tenancy.TenantScope, limit int) ([]accounting.RequestRecord, error) {
	return b.accounting.ListRequests(ctx, scope, limit)
}

func (b *ControlBackend) Usage(ctx context.Context, scope tenancy.TenantScope, requestID string) (*accounting.UsageRecord, error) {
	return b.accounting.GetUsage(ctx, scope, requestID)
}

func (b *ControlBackend) Me(_ context.Context, scope tenancy.TenantScope) (MeView, error) {
	return MeView{TenantID: scope.TenantID, Scopes: []string{scope.TenantID}}, nil
}

func (b *ControlBackend) Health(ctx context.Context, scope tenancy.TenantScope) (HealthView, error) {
	if b.healthView != nil {
		return b.healthView(ctx, scope)
	}
	snapshot := b.snapshotByTenant(ctx, scope)
	if snapshot == nil {
		return HealthView{Ready: false, Drift: "no_active_snapshot"}, nil
	}
	view := HealthView{Ready: true, Drift: "none"}
	for _, item := range snapshot.Providers() {
		view.Providers = append(view.Providers, ProviderHealth{
			ID: item.ID, Type: item.Type, Healthy: item.Status == "enabled",
		})
	}
	if b.circuits != nil {
		for _, deployment := range snapshot.Deployments() {
			state := b.circuits.State(deployment.ID, deployment.CredentialID)
			if state != "" && state != "closed" {
				view.Circuits = append(view.Circuits, CircuitHealth{DeploymentID: deployment.ID, CredentialID: deployment.CredentialID, State: state})
			}
		}
	}
	return view, nil
}

func (b *ControlBackend) Alerts(ctx context.Context, scope tenancy.TenantScope, status string) ([]AlertView, error) {
	if b.alertStore == nil {
		return nil, errNotWired
	}
	items, err := b.alertStore.List(ctx, scope, status)
	if err != nil {
		return nil, err
	}
	result := make([]AlertView, 0, len(items))
	for _, item := range items {
		result = append(result, AlertView{
			ID: item.ID, RuleID: item.RuleID, Severity: item.Severity, Status: item.Status,
			Message: item.Message, FiredAt: item.FiredAt, Evidence: item.Evidence,
		})
	}
	return result, nil
}

func (b *ControlBackend) AlertRules(ctx context.Context, scope tenancy.TenantScope) ([]RuleView, error) {
	if b.alertStore == nil {
		return nil, errNotWired
	}
	rules, err := b.alertStore.ListRules(ctx, scope)
	if err != nil {
		return nil, err
	}
	result := make([]RuleView, 0, len(rules))
	for _, rule := range rules {
		result = append(result, RuleView{
			ID: rule.ID, Name: rule.Name, RuleType: rule.RuleType, Metric: rule.Metric,
			Operator: rule.Operator, Threshold: rule.Threshold, Severity: rule.Severity, Enabled: rule.Enabled,
		})
	}
	return result, nil
}

func (b *ControlBackend) CreateAlertRule(ctx context.Context, scope tenancy.TenantScope, raw json.RawMessage, actorID string) (RuleView, error) {
	if b.alertStore == nil {
		return RuleView{}, errNotWired
	}
	var input RuleView
	if err := json.Unmarshal(raw, &input); err != nil {
		return RuleView{}, err
	}
	// The control backend persists a rule directly; evaluation wiring happens in
	// the composition root. Rule creation is the typed builder surface.
	id := input.ID
	if id == "" {
		id = fmt.Sprintf("rule-%d", time.Now().UnixNano())
	}
	rule := alert.Rule{
		ID: id, TenantID: scope.TenantID, Name: input.Name, RuleType: input.RuleType, Metric: input.Metric,
		Operator: input.Operator, Threshold: input.Threshold, Severity: input.Severity, Enabled: input.Enabled, CreatedBy: actorID,
	}
	if err := b.alertStore.CreateRule(ctx, scope, rule); err != nil {
		return RuleView{}, err
	}
	input.ID = id
	return input, nil
}

func (b *ControlBackend) ImportDefaultAlertRules(ctx context.Context, scope tenancy.TenantScope, actorID string) ([]RuleView, error) {
	if b.alertStore == nil {
		return nil, errNotWired
	}
	existing, err := b.alertStore.ListRules(ctx, scope)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, rule := range existing {
		seen[rule.ID] = true
	}
	var imported []RuleView
	for _, rule := range alert.DefaultRules() {
		if seen[rule.ID] {
			continue
		}
		rule.TenantID = scope.TenantID
		rule.CreatedBy = actorID
		if err := b.alertStore.CreateRule(ctx, scope, rule); err != nil {
			return nil, err
		}
		imported = append(imported, RuleView{ID: rule.ID, Name: rule.Name, RuleType: rule.RuleType, Metric: rule.Metric, Operator: rule.Operator, Threshold: rule.Threshold, Severity: rule.Severity, Enabled: rule.Enabled})
	}
	return imported, nil
}

func (b *ControlBackend) NotificationSettings(ctx context.Context, scope tenancy.TenantScope) (NotificationSettingsView, error) {
	if b.notificationStore == nil {
		return NotificationSettingsView{}, errNotWired
	}
	settings, err := b.notificationStore.GetNotificationSettings(ctx, scope)
	if errors.Is(err, tenancy.ErrNotFound) {
		return NotificationSettingsView{}, nil
	}
	if err != nil {
		return NotificationSettingsView{}, err
	}
	return notificationSettingsView(*settings), nil
}

func (b *ControlBackend) UpdateNotificationSettings(ctx context.Context, scope tenancy.TenantScope, input NotificationSettingsInput, actorID string) (NotificationSettingsView, error) {
	if b.notificationStore == nil {
		return NotificationSettingsView{}, errNotWired
	}
	targets, err := normalizeNotificationTargets(input)
	if err != nil {
		return NotificationSettingsView{}, err
	}
	dedupSeconds := input.DedupSeconds
	if dedupSeconds <= 0 {
		dedupSeconds = 300
	}
	primary := ""
	if len(targets) > 0 {
		primary = targets[0].URL
	}
	now := time.Now().UTC()
	settings := alert.NotificationSettings{
		TenantID: scope.TenantID, WebhookURL: primary, Targets: targets,
		DedupSeconds: dedupSeconds, Enabled: input.Enabled, UpdatedBy: actorID, UpdatedAt: now,
	}
	if err := b.notificationStore.UpsertNotificationSettings(ctx, scope, settings); err != nil {
		return NotificationSettingsView{}, err
	}
	if b.notificationReload != nil {
		if err := b.notificationReload(ctx, settings); err != nil {
			return NotificationSettingsView{}, err
		}
	}
	return notificationSettingsView(settings), nil
}

// normalizeNotificationTargets validates the incoming target set and falls
// back to the legacy single webhook (low severity floor) when only WebhookURL
// is supplied.
func normalizeNotificationTargets(input NotificationSettingsInput) ([]alert.NotificationTarget, error) {
	var targets []alert.NotificationTarget
	switch {
	case len(input.Targets) > 0:
		for _, target := range input.Targets {
			if err := validateWebhookURL(target.URL); err != nil {
				return nil, err
			}
			if target.MinSeverity == "" {
				target.MinSeverity = alert.SeverityLow
			}
			if alert.SeverityRank(target.MinSeverity) == 0 {
				return nil, errors.New("target minSeverity must be one of low|medium|high|critical")
			}
			targets = append(targets, alert.NotificationTarget{URL: target.URL, MinSeverity: target.MinSeverity})
		}
	case input.WebhookURL != "":
		if err := validateWebhookURL(input.WebhookURL); err != nil {
			return nil, err
		}
		targets = []alert.NotificationTarget{{URL: input.WebhookURL, MinSeverity: alert.SeverityLow}}
	default:
		return nil, errors.New("webhookUrl or targets is required")
	}
	return targets, nil
}

func validateWebhookURL(rawURL string) error {
	if rawURL == "" {
		return errors.New("webhookUrl is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.User != nil {
		return errors.New("webhookUrl userinfo is not allowed")
	}
	return egress.ValidateTarget(rawURL, egress.LitePolicy())
}

func (b *ControlBackend) DeleteNotificationSettings(ctx context.Context, scope tenancy.TenantScope, _ string) error {
	if b.notificationStore == nil {
		return errNotWired
	}
	if err := b.notificationStore.DeleteNotificationSettings(ctx, scope); err != nil {
		return err
	}
	if b.notificationReload != nil {
		return b.notificationReload(ctx, alert.NotificationSettings{TenantID: scope.TenantID})
	}
	return nil
}

func notificationSettingsView(settings alert.NotificationSettings) NotificationSettingsView {
	targets := make([]NotificationTargetView, 0, len(settings.Targets))
	for _, target := range settings.Targets {
		targets = append(targets, NotificationTargetView{URL: target.URL, MinSeverity: target.MinSeverity})
	}
	return NotificationSettingsView{
		WebhookURL: settings.WebhookURL, Targets: targets, DedupSeconds: settings.DedupSeconds,
		Enabled: settings.Enabled, UpdatedAt: settings.UpdatedAt,
	}
}

func (b *ControlBackend) AlertAction(ctx context.Context, scope tenancy.TenantScope, alertID, action, actorID string) error {
	if b.alertService == nil {
		return errNotWired
	}
	switch action {
	case "ack":
		return b.alertService.Ack(ctx, scope, alertID, actorID)
	case "resolve":
		return b.alertService.Resolve(ctx, scope, alertID, actorID)
	case "silence":
		until := time.Now().Add(24 * time.Hour)
		return b.alertService.Silence(ctx, scope, alertID, actorID, until)
	default:
		return fmt.Errorf("unknown alert action %q", action)
	}
}

func (b *ControlBackend) ResetCircuit(_ context.Context, _ tenancy.TenantScope, deploymentID, credentialID, _ string) error {
	if b.circuits == nil {
		return errNotWired
	}
	b.circuits.Reset(deploymentID, credentialID)
	return nil
}

func (b *ControlBackend) LiveTail(_ context.Context, _ tenancy.TenantScope) (<-chan LiveEvent, error) {
	if b.live == nil {
		return nil, errNotWired
	}
	ch, _ := b.live.Subscribe()
	return ch, nil
}

func (b *ControlBackend) Rebase(ctx context.Context, scope tenancy.TenantScope, draftID, actorID string) (RebaseResult, error) {
	outcome, err := b.configService.Rebase(ctx, scope, draftID, actorID)
	if err != nil {
		return RebaseResult{}, err
	}
	messageKey := "config.rebased"
	if outcome.Conflict {
		messageKey = "config.rebaseConflict"
	}
	return RebaseResult{Rebased: outcome.Rebased, Conflict: outcome.Conflict, Revision: outcome.Revision, MessageKey: messageKey}, nil
}

func (b *ControlBackend) snapshotByTenant(ctx context.Context, scope tenancy.TenantScope) *runtime.TenantRuntimeSnapshot {
	if b.registry == nil {
		return nil
	}
	snapshot, ok := b.registry.TenantByID(scope.TenantID)
	if !ok {
		return nil
	}
	return snapshot
}

func buildRuntimeResources(snapshot *runtime.TenantRuntimeSnapshot) RuntimeResources {
	view := RuntimeResources{}
	for _, provider := range snapshot.Providers() {
		view.Providers = append(view.Providers, ProviderResource{ID: provider.ID, Type: provider.Type, Status: provider.Status})
	}
	for _, credential := range snapshot.Credentials() {
		view.Credentials = append(view.Credentials, CredentialResource{ID: credential.ID, ProviderID: credential.ProviderID, Status: credential.Status})
	}
	for _, deployment := range snapshot.Deployments() {
		view.Deployments = append(view.Deployments, DeploymentResource{ID: deployment.ID, ProviderID: deployment.ProviderID, Model: deployment.UpstreamModel, Region: deployment.DataRegion, Status: deployment.Status})
	}
	for _, model := range snapshot.LogicalModels() {
		view.LogicalModels = append(view.LogicalModels, LogicalModelResource{ID: model.ID, Alias: model.Alias, RoutePolicyID: model.RoutePolicyID})
	}
	for _, route := range snapshot.Routes() {
		view.Routes = append(view.Routes, RouteResource{ID: route.ID, Strategy: route.Strategy, Version: route.Version})
	}
	for _, item := range snapshot.MCPServers() {
		view.MCPServers = append(view.MCPServers, MCPServerResource{ID: item.ID, URL: item.URL, Status: item.Status})
	}
	for _, item := range snapshot.Tools() {
		view.Tools = append(view.Tools, ToolResource{ID: item.ID, Name: item.Name, Status: item.Status, DataClassification: item.DataClassification})
	}
	for _, item := range snapshot.Agents() {
		view.Agents = append(view.Agents, AgentResource{ID: item.ID, Name: item.Name, Status: item.Status})
	}
	for _, item := range snapshot.BudgetPolicies() {
		view.Budgets = append(view.Budgets, BudgetResource{
			ID: item.ID, ProjectID: item.ProjectID, KeyID: item.KeyID,
			WindowHours: item.WindowHours, TokenLimit: item.TokenLimit,
			Mode: item.Mode, Consistency: item.Consistency,
		})
	}
	cachePolicy := snapshot.CachePolicy()
	view.Cache = CacheResource{
		Enabled:          cachePolicy.Enabled,
		TTLSeconds:       cachePolicy.TTLSeconds,
		NamespaceVersion: cachePolicy.NamespaceVersion,
		Semantic:         SemanticCacheView{Enabled: cachePolicy.Semantic.Enabled, Model: cachePolicy.Semantic.Model, Threshold: cachePolicy.Semantic.Threshold},
	}
	guardrailPolicy := snapshot.GuardrailPolicy()
	view.Guardrail = GuardrailResource{
		Mode:         guardrailPolicy.Mode,
		RuleCount:    len(guardrailPolicy.Rules),
		Judge:        GuardrailJudgeView{Enabled: guardrailPolicy.Judge.Enabled, Model: guardrailPolicy.Judge.Model, Action: guardrailPolicy.Judge.Action},
		Groundedness: GuardrailGroundingView{Enabled: guardrailPolicy.Groundedness.Enabled, MinOverlap: guardrailPolicy.Groundedness.MinOverlap},
	}
	return view
}
