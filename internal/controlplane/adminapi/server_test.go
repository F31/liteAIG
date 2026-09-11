package adminapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/F31/liteAIG/internal/controlplane/config"
	"github.com/F31/liteAIG/internal/controlplane/rbac"
	"github.com/F31/liteAIG/internal/finops/accounting"
	guardraildomain "github.com/F31/liteAIG/internal/guardrail"
	"github.com/F31/liteAIG/internal/identity/apikey"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/platform/webkit"
	"github.com/F31/liteAIG/internal/tenancy"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type publishingBackend struct {
	*fakeBackend
	registry *guardraildomain.PolicyRegistry
}

func (b *publishingBackend) FastPublishGuardrail(ctx context.Context, scope tenancy.TenantScope, policy guardraildomain.Policy, change guardraildomain.ChangeType, actor string) (guardraildomain.Policy, error) {
	return b.registry.FastPublish(ctx, scope, policy, change, actor)
}

type authorizer struct {
	session Session
	err     error
}

func (a authorizer) Authorize(*http.Request) (Session, error) { return a.session, a.err }

func TestAdminAPICompatibilityHeaders(t *testing.T) {
	server := New(AllOf(&fakeBackend{}), authorizer{session: Session{AdminID: "admin", TenantID: "tenant", Role: rbac.RoleViewer}})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/admin/me", nil))
	if rec.Code != http.StatusOK || rec.Header().Get(webkit.APIContractHeader) != webkit.CurrentAPIContract {
		t.Fatalf("current status=%d header=%q body=%s", rec.Code, rec.Header().Get(webkit.APIContractHeader), rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/me", nil)
	req.Header.Set(webkit.APIContractHeader, "1999-01-01")
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "UNSUPPORTED_API_VERSION") {
		t.Fatalf("unsupported status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/me", nil)
	req.Header.Set(webkit.APIContractHeader, webkit.DeprecatedAPIContract)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Deprecation") != "true" || rec.Header().Get("Sunset") != webkit.DeprecatedAPIContractEnd {
		t.Fatalf("deprecated status=%d headers=%v body=%s", rec.Code, rec.Header(), rec.Body.String())
	}
}

func TestTenantScopedRoutesRequireTenantSession(t *testing.T) {
	server := New(AllOf(&fakeBackend{}), authorizer{session: Session{AdminID: "root", Role: rbac.RoleSystemAdmin}})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/admin/me", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("system admin tenant-scoped status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// fakeUserStore backs the /users and re-auth endpoints with a single account.
type fakeUserStore struct {
	credential LocalCredential
}

func newFakeUserStore(adminID, username, passwordHash string) *fakeUserStore {
	return &fakeUserStore{
		credential: LocalCredential{AdminID: adminID, TenantID: "tenant", Username: username, Role: "tenant_admin", Status: "active", PasswordHash: passwordHash},
	}
}

func (s *fakeUserStore) FindLocalCredential(context.Context, string) (LocalCredential, error) {
	return s.credential, nil
}
func (s *fakeUserStore) FindLocalUserByID(_ context.Context, id string) (LocalCredential, error) {
	if id != s.credential.AdminID {
		return LocalCredential{}, ErrLocalUserNotFound
	}
	return s.credential, nil
}
func (s *fakeUserStore) ResetLocalPassword(context.Context, string, string) error { return nil }
func (s *fakeUserStore) ListLocalUsers(context.Context) ([]LocalUser, error) {
	return []LocalUser{{ID: s.credential.AdminID, Username: s.credential.Username, Role: s.credential.Role, Status: s.credential.Status}}, nil
}
func (s *fakeUserStore) CreateLocalUser(context.Context, LocalUser) (LocalUser, error) {
	return LocalUser{}, nil
}
func (s *fakeUserStore) SetUserRole(context.Context, string, string) (LocalUser, error) {
	return LocalUser{}, nil
}
func (s *fakeUserStore) SetUserStatus(context.Context, string, string) (LocalUser, error) {
	return LocalUser{}, nil
}
func (s *fakeUserStore) SetUserEmail(context.Context, string, string) (LocalUser, error) {
	return LocalUser{}, nil
}
func (s *fakeUserStore) DeleteLocalUser(context.Context, string) error { return nil }
func (s *fakeUserStore) CountActiveAdmins(context.Context) (int, error) {
	return 1, nil
}

type fakePasswordVerifier struct{ password string }

func (v fakePasswordVerifier) Hash(password []byte) (string, error) { return string(password), nil }
func (v fakePasswordVerifier) Verify(password []byte, _ string) (bool, error) {
	return string(password) == v.password, nil
}

type fakeBackend struct {
	scope                 tenancy.TenantScope
	notificationSettings  NotificationSettingsView
	notificationInput     NotificationSettingsInput
	notificationUpdatedBy string
	notificationDeletedBy string
	defaultRulesImported  bool
}

func (b *fakeBackend) Setup(context.Context, json.RawMessage) (WizardSetupResponse, error) {
	return WizardSetupResponse{}, nil
}
func (b *fakeBackend) Tenants(context.Context) ([]TenantSummary, error) {
	return []TenantSummary{{ID: "tenant", PublicRef: "tenant-ref", Name: "Tenant", Status: "active"}}, nil
}
func (b *fakeBackend) CreateTenant(context.Context, TenantCreateInput, string) (TenantSummary, error) {
	return TenantSummary{ID: "tenant-created", PublicRef: "created-ref", Name: "Created", Status: "active"}, nil
}
func (b *fakeBackend) UpdateTenantStatus(context.Context, string, TenantStatusInput, string) (TenantSummary, error) {
	return TenantSummary{ID: "tenant", PublicRef: "tenant-ref", Name: "Tenant", Status: "suspended"}, nil
}
func (b *fakeBackend) GetAuditRetention(_ context.Context, s tenancy.TenantScope) (AuditRetentionView, error) {
	b.scope = s
	return AuditRetentionView{TenantID: s.TenantID, RetentionDays: 365}, nil
}
func (b *fakeBackend) SetAuditRetention(_ context.Context, s tenancy.TenantScope, input AuditRetentionInput, actor string) (AuditRetentionView, error) {
	b.scope = s
	return AuditRetentionView{TenantID: s.TenantID, RetentionDays: input.RetentionDays, UpdatedBy: actor}, nil
}
func (b *fakeBackend) Projects(_ context.Context, s tenancy.TenantScope) ([]ProjectSummary, error) {
	b.scope = s
	return []ProjectSummary{}, nil
}
func (b *fakeBackend) CreateProject(context.Context, tenancy.TenantScope, ProjectCreateInput) (ProjectSummary, error) {
	return ProjectSummary{ID: "project"}, nil
}
func (b *fakeBackend) Dashboard(context.Context, tenancy.TenantScope) (DashboardView, error) {
	return DashboardView{Requests: 1}, nil
}
func (b *fakeBackend) FinOps(context.Context, tenancy.TenantScope) (FinOpsView, error) {
	return FinOpsView{Requests: 1}, nil
}
func (b *fakeBackend) Recommendations(context.Context, tenancy.TenantScope) ([]RecommendationView, error) {
	return nil, nil
}
func (b *fakeBackend) Drafts(context.Context, tenancy.TenantScope) ([]DraftSummary, error) {
	return []DraftSummary{}, nil
}
func (b *fakeBackend) Runtime(context.Context, tenancy.TenantScope) (RuntimeResources, error) {
	return RuntimeResources{}, nil
}
func (b *fakeBackend) ResourceCatalog(context.Context, tenancy.TenantScope) (ResourceCatalogView, error) {
	return ResourceCatalogView{}, nil
}
func (b *fakeBackend) CreateKey(context.Context, tenancy.TenantScope, apikey.CreateInput, string) (*apikey.CreateResult, error) {
	return nil, nil
}
func (b *fakeBackend) RevokeKey(context.Context, tenancy.TenantScope, string, string) error {
	return nil
}
func (b *fakeBackend) ListKeys(context.Context, tenancy.TenantScope) ([]KeyResource, error) {
	return nil, nil
}
func (b *fakeBackend) RevealKey(context.Context, tenancy.TenantScope, string) (string, error) {
	return "", nil
}
func (b *fakeBackend) DiscoverMCPTools(context.Context, tenancy.TenantScope, string) ([]DiscoveredTool, error) {
	return nil, nil
}
func (b *fakeBackend) GetDraft(context.Context, tenancy.TenantScope, string) (*config.Draft, error) {
	return nil, config.ErrDraftNotFound
}
func (b *fakeBackend) CreateDraft(context.Context, tenancy.TenantScope, string) (*config.Draft, error) {
	return nil, config.ErrNoPublishedVersion
}
func (b *fakeBackend) UpdateDraft(context.Context, tenancy.TenantScope, string, int64, config.TenantConfig, string) (*config.Draft, error) {
	return nil, nil
}
func (b *fakeBackend) Diff(context.Context, tenancy.TenantScope, string) ([]config.DiffEntry, error) {
	return nil, nil
}
func (b *fakeBackend) Publish(context.Context, tenancy.TenantScope, string, int64, string) (*config.Version, []config.Diagnostic, error) {
	return nil, nil, nil
}
func (b *fakeBackend) Rollback(context.Context, tenancy.TenantScope, int64, string) (*config.Version, error) {
	return nil, nil
}
func (b *fakeBackend) Versions(context.Context, tenancy.TenantScope) ([]config.Version, error) {
	return nil, nil
}
func (b *fakeBackend) Playground(context.Context, tenancy.TenantScope, PlaygroundRequest) (PlaygroundResponse, error) {
	return PlaygroundResponse{}, nil
}
func (b *fakeBackend) Request(context.Context, tenancy.TenantScope, string) (*accounting.RequestRecord, error) {
	return nil, nil
}
func (b *fakeBackend) Requests(context.Context, tenancy.TenantScope, int) ([]accounting.RequestRecord, error) {
	return nil, nil
}
func (b *fakeBackend) Usage(context.Context, tenancy.TenantScope, string) (*accounting.UsageRecord, error) {
	return nil, nil
}
func (b *fakeBackend) Audit(context.Context, tenancy.TenantScope, int, int) ([]AuditRecord, error) {
	return nil, nil
}
func (b *fakeBackend) Me(context.Context, tenancy.TenantScope) (MeView, error) { return MeView{}, nil }
func (b *fakeBackend) Health(context.Context, tenancy.TenantScope) (HealthView, error) {
	return HealthView{}, nil
}
func (b *fakeBackend) Alerts(context.Context, tenancy.TenantScope, string) ([]AlertView, error) {
	return nil, nil
}
func (b *fakeBackend) AlertRules(context.Context, tenancy.TenantScope) ([]RuleView, error) {
	return nil, nil
}
func (b *fakeBackend) CreateAlertRule(context.Context, tenancy.TenantScope, json.RawMessage, string) (RuleView, error) {
	return RuleView{}, nil
}
func (b *fakeBackend) ImportDefaultAlertRules(context.Context, tenancy.TenantScope, string) ([]RuleView, error) {
	b.defaultRulesImported = true
	return []RuleView{{ID: "dr-budget-soft", Name: "Soft budget threshold crossed"}}, nil
}
func (b *fakeBackend) NotificationSettings(context.Context, tenancy.TenantScope) (NotificationSettingsView, error) {
	return b.notificationSettings, nil
}
func (b *fakeBackend) UpdateNotificationSettings(_ context.Context, _ tenancy.TenantScope, input NotificationSettingsInput, actor string) (NotificationSettingsView, error) {
	b.notificationInput = input
	b.notificationUpdatedBy = actor
	b.notificationSettings = NotificationSettingsView{WebhookURL: input.WebhookURL, Enabled: input.Enabled}
	return b.notificationSettings, nil
}
func (b *fakeBackend) DeleteNotificationSettings(_ context.Context, _ tenancy.TenantScope, actor string) error {
	b.notificationDeletedBy = actor
	b.notificationSettings = NotificationSettingsView{}
	return nil
}
func (b *fakeBackend) AlertAction(context.Context, tenancy.TenantScope, string, string, string) error {
	return nil
}
func (b *fakeBackend) ResetCircuit(context.Context, tenancy.TenantScope, string, string, string) error {
	return nil
}
func (b *fakeBackend) LiveTail(context.Context, tenancy.TenantScope) (<-chan LiveEvent, error) {
	return nil, nil
}
func (b *fakeBackend) Rebase(context.Context, tenancy.TenantScope, string, string) (RebaseResult, error) {
	return RebaseResult{}, nil
}
func (b *fakeBackend) SecurityEvents(context.Context, tenancy.TenantScope, int) ([]SecurityEventView, error) {
	return nil, nil
}
func (b *fakeBackend) ToolCalls(context.Context, tenancy.TenantScope, int) ([]ToolCallView, error) {
	return nil, nil
}
func (b *fakeBackend) Simulate(context.Context, tenancy.TenantScope, SimulateRequest) (SimulateResult, error) {
	return SimulateResult{}, nil
}
func (b *fakeBackend) Delegations(context.Context, tenancy.TenantScope) ([]DelegationGrantView, error) {
	return nil, nil
}
func (b *fakeBackend) GrantDelegation(context.Context, tenancy.TenantScope, DelegationGrantInput, string) (DelegationGrantView, error) {
	return DelegationGrantView{ID: "grant-1"}, nil
}
func (b *fakeBackend) RevokeDelegation(context.Context, tenancy.TenantScope, string, string) error {
	return nil
}
func (b *fakeBackend) Federation(context.Context, tenancy.TenantScope) (FederationView, error) {
	return FederationView{}, nil
}
func (b *fakeBackend) FederationDiscover(context.Context, tenancy.TenantScope, FederationDiscoverInput) (RelationshipView, error) {
	return RelationshipView{Status: "candidate"}, nil
}
func (b *fakeBackend) FederationReview(context.Context, tenancy.TenantScope, string, FederationReviewInput, string) error {
	return nil
}
func (b *fakeBackend) Approvals(context.Context, tenancy.TenantScope, int) ([]ApprovalView, error) {
	return nil, nil
}
func (b *fakeBackend) ApprovalAction(context.Context, tenancy.TenantScope, string, string, string) error {
	return nil
}
func (b *fakeBackend) FederationSuspend(context.Context, tenancy.TenantScope, string, string) error {
	return nil
}
func (b *fakeBackend) AgentGraph(context.Context, tenancy.TenantScope, string) (AgentGraphView, error) {
	return AgentGraphView{}, nil
}
func (b *fakeBackend) PushDeliveries(context.Context, tenancy.TenantScope, string, int) ([]PushDeliveryView, error) {
	return nil, nil
}
func (b *fakeBackend) CreateCredential(context.Context, tenancy.TenantScope, CredentialCreateInput, string) error {
	return errors.New("not wired in test")
}
func (b *fakeBackend) RotateCredential(context.Context, tenancy.TenantScope, string, []byte, string) error {
	return errors.New("not wired in test")
}
func (b *fakeBackend) DisableCredential(context.Context, tenancy.TenantScope, string, string) error {
	return errors.New("not wired in test")
}
func (b *fakeBackend) DeleteCredential(context.Context, tenancy.TenantScope, string, string) error {
	return errors.New("not wired in test")
}
func (b *fakeBackend) PlaygroundStream(context.Context, tenancy.TenantScope, PlaygroundRequest, func(StreamEventView)) (PlaygroundResponse, error) {
	return PlaygroundResponse{}, nil
}
func (b *fakeBackend) FastPublishGuardrail(context.Context, tenancy.TenantScope, guardraildomain.Policy, guardraildomain.ChangeType, string) (guardraildomain.Policy, error) {
	return guardraildomain.Policy{}, errors.New("not wired in test")
}
func TestTenantScopeComesFromSessionAndSecurityHeaders(t *testing.T) {
	backend := &fakeBackend{}
	server := New(AllOf(backend), authorizer{session: Session{AdminID: "admin", TenantID: "trusted"}})
	request := httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil)
	request.Header.Set("X-Tenant-ID", "attacker")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != 200 || backend.scope.TenantID != "trusted" {
		t.Fatalf("response=%d scope=%+v", response.Code, backend.scope)
	}
	if response.Header().Get("Content-Security-Policy") == "" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("headers=%v", response.Header())
	}
}
func TestUnauthorizedResponseUsesStableCode(t *testing.T) {
	server := New(AllOf(&fakeBackend{}), authorizer{err: errors.New("no session")})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil))
	if response.Code != 401 {
		t.Fatalf("status=%d", response.Code)
	}
	var body map[string]any
	if json.Unmarshal(response.Body.Bytes(), &body) != nil {
		t.Fatal("invalid JSON")
	}
}

type recordingEventSinkAdmin struct{ events []contracts.DomainEvent }

func (s *recordingEventSinkAdmin) Emit(_ context.Context, event contracts.DomainEvent) error {
	s.events = append(s.events, event)
	return nil
}

func TestAdminSecurityEventsEmittedOnReauthAndRateLimit(t *testing.T) {
	sess := Session{AdminID: "admin-1", TenantID: "tenant", Role: rbac.RoleTenantAdmin}
	sink := &recordingEventSinkAdmin{}
	backend := &tenantBackend{fakeBackend: &fakeBackend{}}
	server := New(AllOf(backend), authorizer{session: sess}).
		WithAdminRateLimiter(NewAdminRateLimiter(time.Minute, 100, 3)).
		WithUserManagement(newFakeUserStore("admin-1", "admin-1", "hash-of-correct-password"), fakePasswordVerifier{password: "correct-password"}, fakeIDGenerator{}).
		WithEventSink(sink)

	// Reauth failure (missing token) emits a structured security event.
	reauth := httptest.NewRecorder()
	server.ServeHTTP(reauth, httptest.NewRequest(http.MethodPost, "/api/admin/guardrail/publish", bytes.NewReader([]byte(`{"id":"p","change":"tighten","rules":[]}`))))
	if reauth.Code != http.StatusUnauthorized {
		t.Fatalf("reauth status=%d", reauth.Code)
	}
	if len(sink.events) != 1 || sink.events[0].Kind != "admin.security.reauth_required" || sink.events[0].Attributes["account_id"] != "admin-1" || sink.events[0].Attributes["reason"] != "missing_token" {
		t.Fatalf("reauth events = %+v", sink.events)
	}

	// Rate-limit denial emits a structured security event after the cap.
	for i := 0; i < 2; i++ {
		r := httptest.NewRecorder()
		server.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil))
		if r.Code != http.StatusOK {
			t.Fatalf("prereq request %d status=%d", i+1, r.Code)
		}
	}
	limited := httptest.NewRecorder()
	server.ServeHTTP(limited, httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil))
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("limited status=%d", limited.Code)
	}
	if len(sink.events) != 2 || sink.events[1].Kind != "admin.security.rate_limited" || sink.events[1].Attributes["account_id"] != "admin-1" {
		t.Fatalf("rate-limit events = %+v", sink.events)
	}
}

func TestAuthenticatedAdminRoutesAreRateLimited(t *testing.T) {
	server := New(AllOf(&fakeBackend{}), authorizer{session: Session{AdminID: "admin-1", TenantID: "tenant", Role: rbac.RoleTenantAdmin}}).
		WithAdminRateLimiter(NewAdminRateLimiter(time.Minute, 10, 2))

	for i := 0; i < 2; i++ {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("request %d status=%d body=%s", i+1, response.Code, response.Body.String())
		}
	}
	limited := httptest.NewRecorder()
	server.ServeHTTP(limited, httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil))
	if limited.Code != http.StatusTooManyRequests || !strings.Contains(limited.Body.String(), "RATE_LIMITED") {
		t.Fatalf("limited status=%d body=%s", limited.Code, limited.Body.String())
	}
}

func TestAdminRateLimiterSeparatesAdmins(t *testing.T) {
	limiter := NewAdminRateLimiter(time.Minute, 10, 1)
	if !limiter.Allow("127.0.0.1", "admin-1", time.Now()) {
		t.Fatal("first admin unexpectedly limited")
	}
	if limiter.Allow("127.0.0.1", "admin-1", time.Now()) {
		t.Fatal("same admin was not limited")
	}
	if !limiter.Allow("127.0.0.1", "admin-2", time.Now()) {
		t.Fatal("different admin unexpectedly limited")
	}
}

func TestAuditRetentionRoutes(t *testing.T) {
	backend := &fakeBackend{}
	admin := New(AllOf(backend), authorizer{session: Session{AdminID: "admin-1", TenantID: "tenant", Role: rbac.RoleTenantAdmin}})
	viewer := New(AllOf(backend), authorizer{session: Session{AdminID: "viewer-1", TenantID: "tenant", Role: rbac.RoleViewer}})

	get := httptest.NewRecorder()
	admin.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/admin/audit/retention", nil))
	if get.Code != http.StatusOK || backend.scope.TenantID != "tenant" || !strings.Contains(get.Body.String(), "retentionDays") {
		t.Fatalf("get status=%d scope=%+v body=%s", get.Code, backend.scope, get.Body.String())
	}

	set := httptest.NewRecorder()
	admin.ServeHTTP(set, httptest.NewRequest(http.MethodPut, "/api/admin/audit/retention", strings.NewReader(`{"retentionDays":90}`)))
	if set.Code != http.StatusOK || !strings.Contains(set.Body.String(), "90") {
		t.Fatalf("set status=%d body=%s", set.Code, set.Body.String())
	}

	invalid := httptest.NewRecorder()
	admin.ServeHTTP(invalid, httptest.NewRequest(http.MethodPut, "/api/admin/audit/retention", strings.NewReader(`{"retentionDays":-5}`)))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid set status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	denyGet := httptest.NewRecorder()
	viewer.ServeHTTP(denyGet, httptest.NewRequest(http.MethodGet, "/api/admin/audit/retention", nil))
	if denyGet.Code != http.StatusOK {
		t.Fatalf("viewer get status=%d (read access)", denyGet.Code)
	}

	denySet := httptest.NewRecorder()
	viewer.ServeHTTP(denySet, httptest.NewRequest(http.MethodPut, "/api/admin/audit/retention", strings.NewReader(`{"retentionDays":30}`)))
	if denySet.Code != http.StatusForbidden {
		t.Fatalf("viewer set status=%d body=%s", denySet.Code, denySet.Body.String())
	}
}

type tenantBackend struct {
	*fakeBackend
	createdBy string
	updatedBy string
	updatedID string
	status    string
}

func (b *tenantBackend) Tenants(context.Context) ([]TenantSummary, error) {
	return []TenantSummary{{ID: "tenant-1", PublicRef: "tenant-ref", Name: "Tenant", Status: "active"}}, nil
}
func (b *tenantBackend) CreateTenant(_ context.Context, input TenantCreateInput, actor string) (TenantSummary, error) {
	b.createdBy = actor
	return TenantSummary{ID: "tenant-2", PublicRef: input.PublicRef, Name: input.Name, Status: "active"}, nil
}
func (b *tenantBackend) UpdateTenantStatus(_ context.Context, id string, input TenantStatusInput, actor string) (TenantSummary, error) {
	b.updatedBy = actor
	b.updatedID = id
	b.status = input.Status
	return TenantSummary{ID: id, PublicRef: "tenant-ref", Name: "Tenant", Status: input.Status}, nil
}

func TestTenantRoutesAreSystemAdminOnly(t *testing.T) {
	backend := &tenantBackend{fakeBackend: &fakeBackend{}}
	admin := New(AllOf(backend), authorizer{session: Session{AdminID: "admin-1", Role: rbac.RoleSystemAdmin}}).
		WithUserManagement(newFakeUserStore("admin-1", "admin-1", "hash-of-correct-password"), fakePasswordVerifier{password: "correct-password"}, fakeIDGenerator{})
	tenantAdmin := New(AllOf(backend), authorizer{session: Session{AdminID: "tenant-admin-1", TenantID: "tenant", Role: rbac.RoleTenantAdmin}})
	viewer := New(AllOf(backend), authorizer{session: Session{AdminID: "viewer-1", TenantID: "tenant", Role: rbac.RoleViewer}})

	list := httptest.NewRecorder()
	admin.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/admin/tenants", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "tenant-ref") {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}

	create := httptest.NewRecorder()
	admin.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/admin/tenants", strings.NewReader(`{"publicRef":"new-ref","name":"New Tenant","defaultProjectName":"Default"}`)))
	if create.Code != http.StatusOK || backend.createdBy != "admin-1" || !strings.Contains(create.Body.String(), "new-ref") {
		t.Fatalf("create status=%d actor=%q body=%s", create.Code, backend.createdBy, create.Body.String())
	}

	invalid := httptest.NewRecorder()
	admin.ServeHTTP(invalid, httptest.NewRequest(http.MethodPost, "/api/admin/tenants", strings.NewReader(`{"name":"Missing Ref"}`)))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid create status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	update := httptest.NewRecorder()
	updateRequest := httptest.NewRequest(http.MethodPatch, "/api/admin/tenants/tenant-1/status", strings.NewReader(`{"status":"suspended"}`))
	updateRequest.Header.Set("X-Reauth-Token", "correct-password")
	admin.ServeHTTP(update, updateRequest)
	if update.Code != http.StatusOK || backend.updatedBy != "admin-1" || backend.updatedID != "tenant-1" || backend.status != "suspended" {
		t.Fatalf("update status=%d actor=%q id=%q input=%q body=%s", update.Code, backend.updatedBy, backend.updatedID, backend.status, update.Body.String())
	}

	denyReauth := httptest.NewRecorder()
	admin.ServeHTTP(denyReauth, httptest.NewRequest(http.MethodPatch, "/api/admin/tenants/tenant-1/status", strings.NewReader(`{"status":"suspended"}`)))
	if denyReauth.Code != http.StatusUnauthorized {
		t.Fatalf("suspend without reauth status=%d body=%s", denyReauth.Code, denyReauth.Body.String())
	}

	deny := httptest.NewRecorder()
	viewer.ServeHTTP(deny, httptest.NewRequest(http.MethodGet, "/api/admin/tenants", nil))
	if deny.Code != http.StatusForbidden {
		t.Fatalf("viewer list status=%d body=%s", deny.Code, deny.Body.String())
	}

	tenantAdminDeny := httptest.NewRecorder()
	tenantAdmin.ServeHTTP(tenantAdminDeny, httptest.NewRequest(http.MethodGet, "/api/admin/tenants", nil))
	if tenantAdminDeny.Code != http.StatusForbidden {
		t.Fatalf("tenant admin list status=%d body=%s", tenantAdminDeny.Code, tenantAdminDeny.Body.String())
	}
}

func TestSystemConfigAdminSurface(t *testing.T) {
	backend := &systemConfigBackend{fakeBackend: &fakeBackend{}}
	systemAuditCalls := 0
	system := New(AllOf(backend), authorizer{session: Session{AdminID: "sys", Role: rbac.RoleSystemAdmin}}).WithSystemConfig(backend).
		WithUserManagement(newFakeUserStore("sys", "sys", "hash-of-correct-password"), fakePasswordVerifier{password: "correct-password"}, fakeIDGenerator{}).
		WithSystemAudit(func(_ context.Context, actorID, action, resourceType, resourceID string) error {
			systemAuditCalls++
			if actorID != "sys" || action != "system_config.update" || resourceType != "system_config" || resourceID != "1" {
				t.Fatalf("system audit = actor:%s action:%s resource:%s/%s", actorID, action, resourceType, resourceID)
			}
			return nil
		})
	tenantAdmin := New(AllOf(backend), authorizer{session: Session{AdminID: "ta", TenantID: "tenant", Role: rbac.RoleTenantAdmin}}).WithSystemConfig(backend)

	get := httptest.NewRecorder()
	system.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/admin/system/config", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), "tenant_defaults") {
		t.Fatalf("system config GET status=%d body=%s", get.Code, get.Body.String())
	}

	put := httptest.NewRecorder()
	putRequest := httptest.NewRequest(http.MethodPut, "/api/admin/system/config", strings.NewReader(`{"tenant_defaults":{"allowed_data_regions":["eu"],"residency_enforcement":"strict"}}`))
	putRequest.Header.Set("X-Reauth-Token", "correct-password")
	system.ServeHTTP(put, putRequest)
	if put.Code != http.StatusOK || backend.stored.TenantDefaults.ResidencyEnforcement != "strict" || len(backend.stored.TenantDefaults.AllowedDataRegions) != 1 {
		t.Fatalf("system config PUT status=%d body=%s stored=%+v", put.Code, put.Body.String(), backend.stored)
	}
	if systemAuditCalls != 1 {
		t.Fatalf("system audit calls = %d", systemAuditCalls)
	}

	denyReauth := httptest.NewRecorder()
	system.ServeHTTP(denyReauth, httptest.NewRequest(http.MethodPut, "/api/admin/system/config", strings.NewReader(`{"tenant_defaults":{"residency_enforcement":"strict"}}`)))
	if denyReauth.Code != http.StatusUnauthorized {
		t.Fatalf("system config PUT without reauth status=%d body=%s", denyReauth.Code, denyReauth.Body.String())
	}

	invalid := httptest.NewRecorder()
	invalidRequest := httptest.NewRequest(http.MethodPut, "/api/admin/system/config", strings.NewReader(`{"tenant_defaults":{"residency_enforcement":"bogus"}}`))
	invalidRequest.Header.Set("X-Reauth-Token", "correct-password")
	system.ServeHTTP(invalid, invalidRequest)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid system config status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	deny := httptest.NewRecorder()
	tenantAdmin.ServeHTTP(deny, httptest.NewRequest(http.MethodGet, "/api/admin/system/config", nil))
	if deny.Code != http.StatusForbidden {
		t.Fatalf("tenant admin system config status=%d body=%s", deny.Code, deny.Body.String())
	}
}

type systemConfigBackend struct {
	*fakeBackend
	stored config.SystemConfig
}

func (b *systemConfigBackend) SystemConfig(_ context.Context) (config.SystemConfig, error) {
	return b.stored, nil
}

func (b *systemConfigBackend) SetSystemConfig(_ context.Context, document config.SystemConfig, _ string) (config.SystemConfig, error) {
	b.stored = document
	return document, nil
}

func TestSystemAdminCanSwitchTenantSession(t *testing.T) {
	clock := &sessionClock{now: time.Unix(1, 0)}
	manager, err := NewSessionManager(SessionConfig{CookieName: "lia", TTL: time.Hour, Secure: true}, clock, bytes.NewReader(make([]byte, 96)))
	if err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRecorder()
	csrf, err := manager.Create(login, Session{AdminID: "root", Role: rbac.RoleSystemAdmin})
	if err != nil {
		t.Fatal(err)
	}
	backend := &tenantBackend{fakeBackend: &fakeBackend{}}
	server := New(AllOf(backend), manager)

	request := httptest.NewRequest(http.MethodPost, "/api/admin/session/tenant", strings.NewReader(`{"tenantId":"tenant-1"}`))
	request.AddCookie(login.Result().Cookies()[0])
	request.Header.Set("X-CSRF-Token", csrf)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "tenant-1") {
		t.Fatalf("switch status=%d body=%s", response.Code, response.Body.String())
	}
	if session, err := manager.Authorize(request); err != nil || session.TenantID != "tenant-1" {
		t.Fatalf("Authorize after switch = %+v,%v", session, err)
	}
}

type membershipChecker struct{ allowed bool }

func (m membershipChecker) HasActiveTenantMembership(context.Context, string, string) (bool, error) {
	return m.allowed, nil
}

func TestTenantSwitchRequiresMembershipForNonSystemAdmin(t *testing.T) {
	clock := &sessionClock{now: time.Unix(1, 0)}
	manager, err := NewSessionManager(SessionConfig{CookieName: "lia", TTL: time.Hour, Secure: true}, clock, bytes.NewReader(make([]byte, 128)))
	if err != nil {
		t.Fatal(err)
	}
	backend := &tenantBackend{fakeBackend: &fakeBackend{}}
	server := New(AllOf(backend), manager).WithTenantMemberships(membershipChecker{allowed: true})
	login := httptest.NewRecorder()
	csrf, err := manager.Create(login, Session{AdminID: "user-1", Role: rbac.RoleViewer})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/admin/session/tenant", strings.NewReader(`{"tenantId":"tenant-1"}`))
	request.AddCookie(login.Result().Cookies()[0])
	request.Header.Set("X-CSRF-Token", csrf)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("member switch status=%d body=%s", response.Code, response.Body.String())
	}

	deniedServer := New(AllOf(backend), manager).WithTenantMemberships(membershipChecker{})
	deniedLogin := httptest.NewRecorder()
	deniedCSRF, err := manager.Create(deniedLogin, Session{AdminID: "user-2", Role: rbac.RoleViewer})
	if err != nil {
		t.Fatal(err)
	}
	deniedRequest := httptest.NewRequest(http.MethodPost, "/api/admin/session/tenant", strings.NewReader(`{"tenantId":"tenant-1"}`))
	deniedRequest.AddCookie(deniedLogin.Result().Cookies()[0])
	deniedRequest.Header.Set("X-CSRF-Token", deniedCSRF)
	denied := httptest.NewRecorder()
	deniedServer.ServeHTTP(denied, deniedRequest)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("non-member switch status=%d body=%s", denied.Code, denied.Body.String())
	}
}

type delegationBackend struct {
	*fakeBackend
	grantedBy string
	revokedBy string
}

func (b *delegationBackend) Delegations(context.Context, tenancy.TenantScope) ([]DelegationGrantView, error) {
	return []DelegationGrantView{{ID: "grant-1", DelegatorID: "agent-a", DelegateeID: "agent-b", Permissions: []string{"invoice.read"}}}, nil
}
func (b *delegationBackend) GrantDelegation(_ context.Context, _ tenancy.TenantScope, input DelegationGrantInput, actor string) (DelegationGrantView, error) {
	b.grantedBy = actor
	return DelegationGrantView{ID: "grant-2", DelegatorID: input.DelegatorID, DelegateeID: input.DelegateeID, Permissions: input.Permissions}, nil
}
func (b *delegationBackend) RevokeDelegation(_ context.Context, _ tenancy.TenantScope, id, actor string) error {
	b.revokedBy = actor + ":" + id
	return nil
}

func TestDelegationRoutesArePermissionGated(t *testing.T) {
	backend := &delegationBackend{fakeBackend: &fakeBackend{}}
	admin := New(AllOf(backend), authorizer{session: Session{AdminID: "admin-1", TenantID: "tenant", Role: rbac.RoleTenantAdmin}})
	viewer := New(AllOf(backend), authorizer{session: Session{AdminID: "viewer-1", TenantID: "tenant", Role: rbac.RoleViewer}})

	list := httptest.NewRecorder()
	admin.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/admin/delegations", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "grant-1") {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}

	grant := httptest.NewRecorder()
	admin.ServeHTTP(grant, httptest.NewRequest(http.MethodPost, "/api/admin/delegations", strings.NewReader(`{"delegatorId":"agent-a","delegateeId":"agent-b","permissions":["invoice.read"]}`)))
	if grant.Code != http.StatusCreated || backend.grantedBy != "admin-1" {
		t.Fatalf("grant status=%d actor=%q body=%s", grant.Code, backend.grantedBy, grant.Body.String())
	}

	denyGrant := httptest.NewRecorder()
	viewer.ServeHTTP(denyGrant, httptest.NewRequest(http.MethodPost, "/api/admin/delegations", strings.NewReader(`{"delegatorId":"agent-a","delegateeId":"agent-b","permissions":["invoice.read"]}`)))
	if denyGrant.Code != http.StatusForbidden {
		t.Fatalf("viewer grant status=%d body=%s", denyGrant.Code, denyGrant.Body.String())
	}

	revoke := httptest.NewRecorder()
	admin.ServeHTTP(revoke, httptest.NewRequest(http.MethodDelete, "/api/admin/delegations/grant-1", nil))
	if revoke.Code != http.StatusOK || backend.revokedBy != "admin-1:grant-1" {
		t.Fatalf("revoke status=%d actor=%q body=%s", revoke.Code, backend.revokedBy, revoke.Body.String())
	}
}

type federationViewBackend struct{ *fakeBackend }

func (b *federationViewBackend) Federation(context.Context, tenancy.TenantScope) (FederationView, error) {
	return FederationView{PushOutbox: A2APushOutboxView{Pending: 2, Sending: 1, Delivered: 3, Failed: 4}}, nil
}

type pushDeliveriesBackend struct {
	*fakeBackend
	lastStatus string
}

func (b *pushDeliveriesBackend) PushDeliveries(_ context.Context, _ tenancy.TenantScope, status string, _ int) ([]PushDeliveryView, error) {
	b.lastStatus = status
	return []PushDeliveryView{{ID: "push-1", TaskID: "task-1", Status: status, Attempts: 3, MaxAttempts: 3, LastError: "callback status 503"}}, nil
}

func TestPushOutboxDeliveriesDrillDownIsSanitized(t *testing.T) {
	server := New(AllOf(&pushDeliveriesBackend{fakeBackend: &fakeBackend{}}), authorizer{session: Session{AdminID: "admin", TenantID: "tenant"}})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/admin/federation/push-outbox?status=delivered", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var items []PushDeliveryView
	if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "push-1" || items[0].TaskID != "task-1" || items[0].LastError != "callback status 503" || items[0].Status != "delivered" {
		t.Fatalf("items = %+v", items)
	}
	raw := response.Body.String()
	for _, forbidden := range []string{"sealed:url", "sealed:payload", "Bearer ", "cb-token"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("drill-down leaked %q: %s", forbidden, raw)
		}
	}
}

func TestFederationViewIncludesPushOutboxSummary(t *testing.T) {
	server := New(AllOf(&federationViewBackend{fakeBackend: &fakeBackend{}}), authorizer{session: Session{AdminID: "admin", TenantID: "tenant"}})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/admin/federation", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		PushOutbox A2APushOutboxView `json:"pushOutbox"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.PushOutbox.Pending != 2 || body.PushOutbox.Sending != 1 || body.PushOutbox.Delivered != 3 || body.PushOutbox.Failed != 4 {
		t.Fatalf("pushOutbox = %+v", body.PushOutbox)
	}
}

func TestNotificationSettingsRoutes(t *testing.T) {
	backend := &fakeBackend{notificationSettings: NotificationSettingsView{WebhookURL: "https://example.com/hook", Enabled: true}}
	server := New(AllOf(backend), authorizer{session: Session{AdminID: "admin", TenantID: "tenant"}})
	get := httptest.NewRecorder()
	server.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/admin/alerts/notifications", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"webhookUrl":"https://example.com/hook"`) {
		t.Fatalf("get status=%d body=%s", get.Code, get.Body.String())
	}

	body := []byte(`{"webhookUrl":"https://notify.example/hook","enabled":true}`)
	put := httptest.NewRecorder()
	server.ServeHTTP(put, httptest.NewRequest(http.MethodPut, "/api/admin/alerts/notifications", bytes.NewReader(body)))
	if put.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", put.Code, put.Body.String())
	}
	if backend.notificationInput.WebhookURL != "https://notify.example/hook" || !backend.notificationInput.Enabled || backend.notificationUpdatedBy != "admin" {
		t.Fatalf("input=%+v actor=%q", backend.notificationInput, backend.notificationUpdatedBy)
	}
	if strings.Contains(put.Body.String(), `"requiresRestart":true`) {
		t.Fatalf("unexpected requiresRestart: %s", put.Body.String())
	}

	del := httptest.NewRecorder()
	server.ServeHTTP(del, httptest.NewRequest(http.MethodDelete, "/api/admin/alerts/notifications", nil))
	if del.Code != http.StatusOK || backend.notificationDeletedBy != "admin" {
		t.Fatalf("delete status=%d actor=%q body=%s", del.Code, backend.notificationDeletedBy, del.Body.String())
	}
}

func TestImportDefaultAlertRulesRoute(t *testing.T) {
	backend := &fakeBackend{}
	server := New(AllOf(backend), authorizer{session: Session{AdminID: "admin", TenantID: "tenant"}})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/admin/alerts/rules/import-defaults", nil))
	if response.Code != http.StatusOK || !backend.defaultRulesImported {
		t.Fatalf("status=%d imported=%t body=%s", response.Code, backend.defaultRulesImported, response.Body.String())
	}
}

func TestGuardrailFastPublishRequiresReauth(t *testing.T) {
	backend := &publishingBackend{fakeBackend: &fakeBackend{}, registry: guardraildomain.NewPolicyRegistry(nil)}
	store := newFakeUserStore("admin", "admin", "hash-of-correct-password")
	verifier := fakePasswordVerifier{password: "correct-password"}
	server := New(AllOf(backend), authorizer{session: Session{AdminID: "admin", TenantID: "tenant"}}).
		WithUserManagement(store, verifier, fakeIDGenerator{})
	body := []byte(`{"id":"policy","change":"tighten","rules":[{"ID":"deny","Kind":"keyword","Pattern":"blocked","Action":"block"}]}`)

	// No reauth header: rejected.
	request := httptest.NewRequest(http.MethodPost, "/api/admin/guardrail/publish", bytes.NewReader(body))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("without reauth status=%d", response.Code)
	}

	// A wrong password is not accepted even though the header is present.
	request = httptest.NewRequest(http.MethodPost, "/api/admin/guardrail/publish", bytes.NewReader(body))
	request.Header.Set("X-Reauth-Token", "wrong-password")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("with wrong reauth password status=%d body=%s", response.Code, response.Body.String())
	}

	// Only the session account's verified current password re-authenticates.
	request = httptest.NewRequest(http.MethodPost, "/api/admin/guardrail/publish", bytes.NewReader(body))
	request.Header.Set("X-Reauth-Token", "correct-password")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("with valid reauth password status=%d body=%s", response.Code, response.Body.String())
	}
}

type fakeIDGenerator struct{}

func (fakeIDGenerator) New() (string, error) { return "id", nil }

type liveBackend struct {
	*fakeBackend
	events chan LiveEvent
}

func (b *liveBackend) LiveTail(context.Context, tenancy.TenantScope) (<-chan LiveEvent, error) {
	return b.events, nil
}

func TestLiveTailPayloadContainsSummaryOnly(t *testing.T) {
	events := make(chan LiveEvent, 1)
	events <- LiveEvent{RequestID: "request-1", Outcome: "success", DeploymentID: "deployment", LatencyMS: 12}
	close(events)
	server := New(AllOf(&liveBackend{fakeBackend: &fakeBackend{}, events: events}), authorizer{session: Session{AdminID: "admin", TenantID: "tenant"}})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/admin/live", nil))
	body := response.Body.String()
	if response.Code != 200 || !strings.Contains(body, "request-1") {
		t.Fatalf("response=%d %s", response.Code, body)
	}
	lower := strings.ToLower(body)
	if strings.Contains(lower, "prompt") || strings.Contains(lower, "response_body") || strings.Contains(lower, "authorization") {
		t.Fatalf("Live Tail leaked sensitive content: %s", body)
	}
}
