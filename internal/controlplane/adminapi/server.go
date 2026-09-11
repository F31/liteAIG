// Package adminapi exposes the scoped Phase 0 Control Plane HTTP contract.
//
// The server is a webkit engine (internal/platform/webkit): routing on the
// Go 1.22 ServeMux, authn/authz as middleware groups, and a pluggable error
// handler mapping domain errors to the Console JSON error contract. Routes
// are registered by domain in the routes_*.go files.
package adminapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/F31/liteAIG/internal/controlplane/backend"
	"github.com/F31/liteAIG/internal/controlplane/config"
	"github.com/F31/liteAIG/internal/controlplane/rbac"
	"github.com/F31/liteAIG/internal/controlplane/setup"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/platform/webkit"
	"github.com/F31/liteAIG/internal/tenancy"
)

type Session struct{ AdminID, TenantID, Username, Role string }

// EffectiveRole normalizes the session role. Sessions created before role
// tracking (legacy single-admin installs) act as tenant admins.
func (s Session) EffectiveRole() string {
	if s.Role == "" {
		return rbac.RoleTenantAdmin
	}
	return s.Role
}

type Authorizer interface {
	Authorize(*http.Request) (Session, error)
}

type tenantSessionSwitcher interface {
	SwitchTenant(*http.Request, string) (Session, error)
}

type TenantMembershipChecker interface {
	HasActiveTenantMembership(context.Context, string, string) (bool, error)
}

type PlaygroundRequest = backend.PlaygroundRequest
type PlaygroundResponse = backend.PlaygroundResponse

// ctxSession is the Context key under which requireAuth stores the session.
const ctxSession = "session"

// adminCSP is the strict content policy for the Console origin.
const adminCSP = "default-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'self'"

type Server struct {
	setupSvc          SetupService
	tenantSvc         TenantService
	projectSvc        ProjectService
	dashboardSvc      DashboardService
	keySvc            KeyService
	configSvc         ConfigService
	runtimeSvc        RuntimeService
	playgroundSvc     PlaygroundService
	requestSvc        RequestService
	alertSvc          AlertService
	securitySvc       SecurityService
	delegationSvc     DelegationService
	federationSvc     FederationService
	identitySvc       IdentityService
	evidenceSvc       EvidenceService
	auditRetentionSvc AuditRetentionService

	// Optional capabilities: nil makes the dependent route answer
	// 501 NOT_IMPLEMENTED.
	credentials        CredentialOperator
	playgroundStreamer PlaygroundStreamer
	guardrails         GuardrailPublisher
	systemConfigSvc    SystemConfigService

	authorizer     Authorizer
	engine         *webkit.Engine
	userStore      LocalCredentialStore
	userPasswords  PasswordVerifier
	userIDs        UserIDGenerator
	memberships    TenantMembershipChecker
	bootstrapToken string
	adminLimiter   *AdminRateLimiter

	// auditFn records sensitive actions (wired by the composition root with a
	// tenant-scoped writer); nil disables auditing.
	auditFn func(ctx context.Context, tenantID, actorID, action, resourceType, resourceID string) error
	// systemAuditFn records global system_admin actions that intentionally have
	// no tenant scope; nil disables auditing.
	systemAuditFn func(ctx context.Context, actorID, action, resourceType, resourceID string) error
	// eventSink is the optional structured security-event sink (DomainEvent)
	// used for credential/control-plane signals such as reauth failures and
	// rate-limit denials. nil disables.
	eventSink contracts.EventSink
	// sessionInvalidator voids every live session of an account (wired with
	// the session manager); nil disables proactive invalidation.
	sessionInvalidator func(adminID string)
}

func New(svc Services, authorizer Authorizer) *Server {
	s := &Server{
		setupSvc:           svc.Setup,
		tenantSvc:          svc.Tenant,
		projectSvc:         svc.Project,
		dashboardSvc:       svc.Dashboard,
		keySvc:             svc.Key,
		configSvc:          svc.Config,
		runtimeSvc:         svc.Runtime,
		playgroundSvc:      svc.Playground,
		requestSvc:         svc.Request,
		alertSvc:           svc.Alert,
		securitySvc:        svc.Security,
		delegationSvc:      svc.Delegation,
		federationSvc:      svc.Federation,
		identitySvc:        svc.Identity,
		evidenceSvc:        svc.Evidence,
		auditRetentionSvc:  svc.AuditRetention,
		credentials:        svc.Credentials,
		playgroundStreamer: svc.PlaygroundStream,
		guardrails:         svc.Guardrails,
		authorizer:         authorizer,
		adminLimiter:       NewAdminRateLimiter(time.Minute, 600, 120),
	}
	s.engine = webkit.New().Use(
		webkit.Recover(),
		webkit.SecurityHeaders([2]string{"Content-Security-Policy", adminCSP}),
		webkit.APICompatibility(),
	)
	s.engine.ErrorHandler(s.handleError)
	s.registerRoutes()
	return s
}

// Engine exposes the kernel for plugin registration (session endpoints).
func (s *Server) Engine() *webkit.Engine { return s.engine }

// WithBootstrapToken arms the one-time bootstrap gate on POST /api/admin/setup.
// The wizard is otherwise anonymous: without a gate, anyone who can reach the
// admin port before initialization completes can claim the first admin or
// point the provider probe at an attacker-controlled endpoint. The gate
// admits loopback requests (the local console) and, when a token is set,
// requests carrying it in the X-Bootstrap-Token header (a remote console whose
// operator read the token from the server log).
func (s *Server) WithBootstrapToken(token string) *Server {
	s.bootstrapToken = token
	return s
}

func (s *Server) WithAdminRateLimiter(limiter *AdminRateLimiter) *Server {
	s.adminLimiter = limiter
	return s
}

// WithAudit wires the tenant-scoped audit writer used by sensitive handlers.
func (s *Server) WithAudit(write func(ctx context.Context, tenantID, actorID, action, resourceType, resourceID string) error) *Server {
	s.auditFn = write
	return s
}

// WithSystemAudit wires the system-scoped audit writer used by global
// system_admin-only mutations.
func (s *Server) WithSystemAudit(write func(ctx context.Context, actorID, action, resourceType, resourceID string) error) *Server {
	s.systemAuditFn = write
	return s
}

// WithEventSink wires the structured security-event sink (DomainEvent) used by
// credential and control-plane signals (reauth failures, rate-limit denials).
func (s *Server) WithEventSink(sink contracts.EventSink) *Server {
	s.eventSink = sink
	return s
}

// WithSessionInvalidation wires a sink for "void every session of this
// account", used after role/status/email/password changes.
func (s *Server) WithSessionInvalidation(invalidate func(adminID string)) *Server {
	s.sessionInvalidator = invalidate
	return s
}

func (s *Server) WithTenantMemberships(checker TenantMembershipChecker) *Server {
	s.memberships = checker
	return s
}

// WithSystemConfig wires the system_admin-only system config surface. A nil
// service makes the routes answer 501 NOT_IMPLEMENTED.
func (s *Server) WithSystemConfig(svc SystemConfigService) *Server {
	s.systemConfigSvc = svc
	return s
}

// recordAudit records a successful sensitive mutation on behalf of the acting
// session. A failed audit write is logged but does not fail the request: the
// mutation is already committed, and surfacing a misleading error would make
// operators redo a change that actually happened.
func (s *Server) recordAudit(c *webkit.Context, action, resourceType, resourceID string) {
	if s.auditFn == nil {
		return
	}
	session := sessionFrom(c)
	if session.TenantID == "" || session.AdminID == "" {
		return
	}
	if err := s.auditFn(c.Request().Context(), session.TenantID, session.AdminID, action, resourceType, resourceID); err != nil {
		log.Printf("audit write failed (%s resource=%s): %v", action, resourceID, err)
	}
}

func (s *Server) recordSystemAudit(c *webkit.Context, action, resourceType, resourceID string) {
	if s.systemAuditFn == nil {
		return
	}
	session := sessionFrom(c)
	if session.AdminID == "" {
		return
	}
	if err := s.systemAuditFn(c.Request().Context(), session.AdminID, action, resourceType, resourceID); err != nil {
		log.Printf("system audit write failed (%s resource=%s): %v", action, resourceID, err)
	}
}

// requireReauth forces a step-up re-authentication for high-risk admin
// mutations. The client must present X-Reauth-Token matching the acting
// session account's current local password; IdP-only sessions without a local
// credential cannot step up and the operation is refused.
func (s *Server) requireReauth(c *webkit.Context) error {
	reauth := c.Request().Header.Get("X-Reauth-Token")
	if reauth == "" {
		s.emitSecurityEvent(c, "admin.security.reauth_required", map[string]string{"reason": "missing_token"})
		return webkit.NewAPIError(http.StatusUnauthorized, "REAUTH_REQUIRED", nil)
	}
	session := sessionFrom(c)
	if !s.usersReady() {
		return webkit.NewAPIError(http.StatusForbidden, "REAUTH_UNAVAILABLE", nil)
	}
	credential, err := s.userStore.FindLocalUserByID(c.Request().Context(), session.AdminID)
	if err != nil || credential.Status != "active" {
		return webkit.NewAPIError(http.StatusForbidden, "REAUTH_UNAVAILABLE", nil)
	}
	reauthBytes := []byte(reauth)
	defer clear(reauthBytes)
	matched, err := s.userPasswords.Verify(reauthBytes, credential.PasswordHash)
	if err != nil || !matched {
		s.emitSecurityEvent(c, "admin.security.reauth_failed", map[string]string{"reason": "password_mismatch"})
		return webkit.NewAPIError(http.StatusUnauthorized, "REAUTH_REQUIRED", map[string]any{"reason": "password_mismatch"})
	}
	return nil
}

// emitSecurityEvent publishes a best-effort structured security DomainEvent.
// A missing ID generator or sink disables it; failures never fail the request.
func (s *Server) emitSecurityEvent(c *webkit.Context, kind string, attributes map[string]string) {
	if s.eventSink == nil {
		return
	}
	generator := s.userIDs
	if generator == nil {
		return
	}
	id, err := generator.New()
	if err != nil || id == "" {
		return
	}
	session := sessionFrom(c)
	attributes["account_id"] = session.AdminID
	_ = s.eventSink.Emit(c.Request().Context(), contracts.DomainEvent{
		ID: id, Kind: kind, OccurredAt: time.Now(), TenantID: session.TenantID, Attributes: attributes,
	})
}

// invalidateSessions voids every live session of an account after a security
// or identity mutation (role change, disable, email change, password reset).
func (s *Server) invalidateSessions(adminID string) {
	if s.sessionInvalidator == nil || adminID == "" {
		return
	}
	s.sessionInvalidator(adminID)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.engine.ServeHTTP(w, r)
}

// requireAuth resolves the session (cookie + CSRF) and stores it on the
// context; every scoped route group starts with it.
func (s *Server) requireAuth(next webkit.Handler) webkit.Handler {
	return func(c *webkit.Context) error {
		session, err := s.authorizer.Authorize(c.Request())
		if err != nil || session.AdminID == "" {
			return webkit.NewAPIError(http.StatusUnauthorized, "UNAUTHORIZED", nil)
		}
		c.Set(ctxSession, session)
		return next(c)
	}
}

// requireRole restricts a route to one session role.
func (s *Server) requireRole(role string) webkit.Middleware {
	return func(next webkit.Handler) webkit.Handler {
		return func(c *webkit.Context) error {
			if sessionFrom(c).EffectiveRole() != role {
				return webkit.NewAPIError(http.StatusForbidden, "ROLE_FORBIDDEN", nil)
			}
			return next(c)
		}
	}
}

func (s *Server) requireAnyRole(roles ...string) webkit.Middleware {
	allowed := make(map[string]bool, len(roles))
	for _, role := range roles {
		allowed[role] = true
	}
	return func(next webkit.Handler) webkit.Handler {
		return func(c *webkit.Context) error {
			if !allowed[sessionFrom(c).EffectiveRole()] {
				return webkit.NewAPIError(http.StatusForbidden, "ROLE_FORBIDDEN", nil)
			}
			return next(c)
		}
	}
}

func (s *Server) requireTenantScope(next webkit.Handler) webkit.Handler {
	return func(c *webkit.Context) error {
		if sessionFrom(c).TenantID == "" {
			return webkit.NewAPIError(http.StatusUnauthorized, "UNAUTHORIZED", nil)
		}
		return next(c)
	}
}

// requirePermission restricts a route using the rbac permission matrix.
func (s *Server) requirePermission(permission string) webkit.Middleware {
	return func(next webkit.Handler) webkit.Handler {
		return func(c *webkit.Context) error {
			if err := rbac.Require(sessionFrom(c).EffectiveRole(), permission); err != nil {
				return webkit.NewAPIError(http.StatusForbidden, "ROLE_FORBIDDEN", nil)
			}
			return next(c)
		}
	}
}

func (s *Server) rateLimitAdmin(next webkit.Handler) webkit.Handler {
	return func(c *webkit.Context) error {
		limiter := s.adminLimiter
		if limiter == nil {
			return next(c)
		}
		session := sessionFrom(c)
		if !limiter.Allow(clientIP(c.Request().RemoteAddr), session.AdminID, time.Now()) {
			s.emitSecurityEvent(c, "admin.security.rate_limited", map[string]string{"reason": "admin_request_flood"})
			return webkit.NewAPIError(http.StatusTooManyRequests, "RATE_LIMITED", map[string]any{"retryAfterSeconds": int(limiter.Window().Seconds())})
		}
		return next(c)
	}
}

// handleError renders the Console error contract and logs 5xx causes.
func (s *Server) handleError(c *webkit.Context, err error) {
	status, code, params := s.mapError(err)
	if status >= http.StatusInternalServerError {
		log.Printf("admin api internal error: %v", err)
	}
	_ = c.JSON(status, webkit.ErrorBody(code, params))
}

// mapError translates domain errors into the (status, code, params) triple.
// APIErrors carry their own mapping; sentinel errors keep the historical
// contract.
func (s *Server) mapError(err error) (int, string, map[string]any) {
	var apiErr *webkit.APIError
	if errors.As(err, &apiErr) && apiErr.Code != "" {
		return apiErr.Status, apiErr.Code, apiErr.Params
	}
	switch {
	case errors.Is(err, tenancy.ErrNotFound) || errors.Is(err, config.ErrDraftNotFound):
		return http.StatusNotFound, "NOT_FOUND", nil
	case errors.Is(err, config.ErrRevisionConflict):
		return http.StatusConflict, "REVISION_CONFLICT", nil
	case errors.Is(err, config.ErrNoPublishedVersion):
		return http.StatusConflict, "NO_PUBLISHED_VERSION", nil
	case errors.Is(err, setup.ErrInvalidInput):
		return http.StatusBadRequest, "INVALID_REQUEST", nil
	case errors.Is(err, setup.ErrAlreadyInitialized):
		return http.StatusConflict, "ALREADY_INITIALIZED", nil
	case errors.Is(err, setup.ErrProviderConnection):
		return http.StatusBadGateway, "SETUP_PROVIDER_CONNECTION_FAILED", nil
	case errors.Is(err, setup.ErrProviderAuth):
		return http.StatusBadRequest, "SETUP_PROVIDER_AUTH_FAILED", nil
	case errors.Is(err, setup.ErrProviderError):
		return http.StatusBadGateway, "SETUP_PROVIDER_ERROR", nil
	case errors.Is(err, setup.ErrModelNotFound):
		return http.StatusBadRequest, "SETUP_MODEL_NOT_FOUND", nil
	case errors.Is(err, backend.ErrRuntimeRefreshFailed):
		return http.StatusInternalServerError, "RUNTIME_REFRESH_FAILED", nil
	}
	return http.StatusInternalServerError, "INTERNAL_ERROR", nil
}

func sessionFrom(c *webkit.Context) Session {
	value, _ := c.Get(ctxSession)
	session, _ := value.(Session)
	return session
}

func scopeOf(c *webkit.Context) tenancy.TenantScope {
	return tenancy.TenantScope{TenantID: sessionFrom(c).TenantID}
}

// jsonResult is the standard success path: 200 with the backend value, or
// the mapped error.
func jsonResult(c *webkit.Context, value any, err error) error {
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, value)
}

func invalidRequest() *webkit.APIError {
	return webkit.NewAPIError(http.StatusBadRequest, "INVALID_REQUEST", nil)
}

func notImplemented() *webkit.APIError {
	return webkit.NewAPIError(http.StatusNotImplemented, "NOT_IMPLEMENTED", nil)
}

func internalError() *webkit.APIError {
	return webkit.NewAPIError(http.StatusInternalServerError, "INTERNAL_ERROR", nil)
}

func (s *Server) registerRoutes() {
	e := s.engine
	e.Handle("POST /api/admin/setup", s.setup)

	read := e.Group("/api/admin", s.requireAuth, s.requireTenantScope, s.rateLimitAdmin)
	session := e.Group("/api/admin", s.requireAuth, s.rateLimitAdmin)
	system := e.Group("/api/admin", s.requireAuth, s.rateLimitAdmin, s.requireRole(rbac.RoleSystemAdmin))
	admin := e.Group("/api/admin", s.requireAuth, s.requireTenantScope, s.rateLimitAdmin, s.requireAnyRole(rbac.RoleSystemAdmin, rbac.RoleTenantAdmin))

	system.Handle("GET /tenants", s.tenants)
	system.Handle("POST /tenants", s.createTenant)
	system.Handle("PATCH /tenants/{id}/status", s.updateTenantStatus)
	system.Handle("GET /system/config", s.systemConfig)
	system.Handle("PUT /system/config", s.setSystemConfig)
	session.Handle("POST /session/tenant", s.switchTenant)
	read.Handle("GET /projects", s.projects)
	admin.Handle("POST /projects", s.createProject)
	read.Handle("GET /dashboard", s.dashboard)
	read.Handle("GET /finops", s.finops)
	read.Handle("GET /recommendations", s.recommendations)
	read.Handle("GET /config/drafts", s.drafts)
	admin.Handle("POST /config/drafts", s.createDraft)
	read.Handle("GET /runtime", s.runtime)
	read.Handle("GET /resource-catalog", s.resourceCatalog)
	admin.Handle("POST /keys", s.createKey)
	read.Handle("GET /keys", s.listKeys)
	admin.Handle("GET /keys/{id}", s.revealKey)
	admin.Handle("POST /credentials", s.createCredential)
	admin.Handle("POST /keys/{id}/revoke", s.revokeKey)
	admin.Handle("POST /mcp-servers/{id}/discover", s.discoverMCPTools)
	admin.Handle("POST /credentials/{id}/rotate", s.rotateCredential)
	admin.Handle("POST /credentials/{id}/disable", s.disableCredential)
	admin.Handle("DELETE /credentials/{id}", s.deleteCredential)
	read.Handle("GET /config/drafts/{id}", s.getDraft)
	admin.Handle("PUT /config/drafts/{id}", s.updateDraft)
	read.Handle("GET /config/drafts/{id}/diff", s.diff)
	admin.Handle("POST /config/drafts/{id}/publish", s.publish)
	admin.Handle("POST /config/rollback", s.rollback)
	read.Handle("GET /config/versions", s.versions)
	admin.Handle("POST /playground", s.playground)
	read.Handle("GET /requests/{id}", s.request)
	read.Handle("GET /requests", s.requests)
	read.Handle("GET /usage/{id}", s.usage)
	read.Handle("GET /audit", s.audit)
	read.Handle("GET /audit/retention", s.auditRetention)
	admin.Handle("PUT /audit/retention", s.setAuditRetention)
	e.Group("/api/admin", s.requireAuth, s.requireTenantScope, s.rateLimitAdmin, s.requirePermission(rbac.PermEvidenceExport)).
		Handle("GET /evidence", s.evidence)
	read.Handle("GET /me", s.me)
	read.Handle("GET /health", s.health)
	read.Handle("GET /alerts", s.alerts)
	read.Handle("GET /alerts/rules", s.alertRules)
	read.Handle("GET /alerts/notifications", s.notificationSettings)
	admin.Handle("POST /alerts/rules", s.createAlertRule)
	admin.Handle("POST /alerts/rules/import-defaults", s.importDefaultAlertRules)
	admin.Handle("PUT /alerts/notifications", s.updateNotificationSettings)
	admin.Handle("DELETE /alerts/notifications", s.deleteNotificationSettings)
	admin.Handle("POST /alerts/{id}/action", s.alertAction)
	admin.Handle("POST /circuits/reset", s.resetCircuit)
	read.Handle("GET /live", s.liveTail)
	admin.Handle("POST /config/rebase", s.rebase)
	read.Handle("GET /governance", s.governance)
	admin.Handle("POST /guardrail/publish", s.fastPublishGuardrail)
	read.Handle("GET /security-events", s.securityEvents)
	read.Handle("GET /tool-calls", s.toolCalls)
	admin.Handle("POST /simulator", s.simulate)
	read.Handle("GET /delegations", s.delegations)
	e.Group("/api/admin", s.requireAuth, s.requireTenantScope, s.rateLimitAdmin, s.requirePermission(rbac.PermDelegationGrant)).
		Handle("POST /delegations", s.grantDelegation)
	e.Group("/api/admin", s.requireAuth, s.requireTenantScope, s.rateLimitAdmin, s.requirePermission(rbac.PermDelegationRevoke)).
		Handle("DELETE /delegations/{id}", s.revokeDelegation)
	read.Handle("GET /federation", s.federation)
	read.Handle("GET /federation/push-outbox", s.pushOutboxDeliveries)
	read.Handle("GET /approvals", s.approvals)
	e.Group("/api/admin", s.requireAuth, s.requireTenantScope, s.rateLimitAdmin, s.requirePermission(rbac.PermApprovalDecide)).
		Handle("POST /approvals/{id}/action", s.approvalAction)
	e.Group("/api/admin", s.requireAuth, s.requireTenantScope, s.rateLimitAdmin, s.requirePermission(rbac.PermExternalAgentSuspend)).
		Handle("POST /federation/{id}/suspend", s.federationSuspend)
	e.Group("/api/admin", s.requireAuth, s.requireTenantScope, s.rateLimitAdmin, s.requirePermission(rbac.PermExternalAgentSuspend)).
		Handle("POST /federation/discover", s.federationDiscover)
	e.Group("/api/admin", s.requireAuth, s.requireTenantScope, s.rateLimitAdmin, s.requirePermission(rbac.PermExternalAgentSuspend)).
		Handle("POST /federation/{id}/review", s.federationReview)
	read.Handle("GET /agent-graph/{rootTaskId}", s.agentGraph)
	admin.Handle("GET /users", s.listUsers)
	admin.Handle("POST /users", s.createUser)
	admin.Handle("PATCH /users/{id}/role", s.updateUserRole)
	admin.Handle("PATCH /users/{id}/email", s.updateUserEmail)
	admin.Handle("POST /users/{id}/status", s.updateUserStatus)
	admin.Handle("POST /users/{id}/reset-password", s.resetUserPassword)
	admin.Handle("DELETE /users/{id}", s.deleteUser)
	read.Handle("POST /users/me/email", s.updateMyEmail)
}

func (s *Server) setup(c *webkit.Context) error {
	if err := s.setupGate(c); err != nil {
		return err
	}
	var input json.RawMessage
	if err := c.Bind(&input, 1<<20); err != nil {
		return invalidRequest()
	}
	value, err := s.setupSvc.Setup(c.Request().Context(), input)
	return jsonResult(c, value, err)
}

// setupGate is the bootstrap guard for the anonymous setup endpoint. It
// admits loopback traffic and (when armed) a matching X-Bootstrap-Token;
// everything else is locked out before the wizard can be probed or abused.
func (s *Server) setupGate(c *webkit.Context) error {
	r := c.Request()
	if isLoopbackRequest(r) {
		return nil
	}
	if s.bootstrapToken != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Bootstrap-Token")), []byte(s.bootstrapToken)) == 1 {
		return nil
	}
	return webkit.NewAPIError(http.StatusForbidden, "SETUP_LOCKED", map[string]any{"hint": "open the console on this host or present the X-Bootstrap-Token from the server log"})
}
