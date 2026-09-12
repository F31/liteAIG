package adminapi

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"time"

	"github.com/F31/liteAIG/internal/controlplane/rbac"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/platform/webkit"
)

const minimumResetPasswordSize = 8

type CredentialVerifier interface {
	Verify(context.Context, string, []byte) (Session, error)
}
type SessionEndpoints struct {
	Sessions     *SessionManager
	Verifier     CredentialVerifier
	OIDC         *OIDCLogin
	MaxBodyBytes int64
	// BootstrapToken is the server-log credential for the sessionless
	// emergency reset path; empty disables that path entirely.
	BootstrapToken string
	// Audit records local account lifecycle actions (may be nil).
	Audit func(ctx context.Context, actor, action, resourceID string) error
	// LoginLimiter bounds credential attempts; nil disables throttling.
	LoginLimiter *LoginLimiter
	// EventSink receives redacted authentication security events. EventIDs is
	// required when EventSink is configured; emission is best-effort.
	EventSink contracts.EventSink
	EventIDs  UserIDGenerator
	// PasswordReset serves the email-verified forgot-password flow; nil or
	// disabled keeps the login screen on the local emergency reset only.
	PasswordReset PasswordResetService
	// ResetRateLimiter bounds reset-code requests; nil disables throttling.
	ResetRateLimiter *ResetRateLimiter
}

// Register mounts the session plugin (login/logout/password/OIDC) onto the
// admin engine.
func (e SessionEndpoints) Register(engine *webkit.Engine) {
	engine.Handle("POST /api/admin/session", e.login)
	engine.Handle("DELETE /api/admin/session", e.logout)
	engine.Handle("POST /api/admin/password/reset", e.resetPassword)
	engine.Handle("POST /api/admin/password/change", e.changePassword)
	e.registerPasswordReset(engine)
	if e.OIDC != nil {
		e.OIDC.register(engine)
	} else {
		engine.Handle("GET /api/admin/oidc/config", func(c *webkit.Context) error {
			return c.JSON(http.StatusOK, map[string]bool{"enabled": false})
		})
	}
}

// resetPassword is the loopback-only emergency reset. Loopback alone is not
// authorization (any co-located process or host-network container can reach
// it), so the request must additionally carry either a signed-in tenant-admin
// session (cookie + CSRF enforced by Authorize) or the bootstrap token printed
// to the server log. Every reset is recorded in the audit trail.
func (e SessionEndpoints) resetPassword(c *webkit.Context) error {
	r := c.Request()
	if !isLoopbackRequest(r) {
		return webkit.NewAPIError(http.StatusForbidden, "FORBIDDEN", nil)
	}
	if e.MaxBodyBytes <= 0 {
		return invalidRequest()
	}
	var input struct {
		Username    string `json:"username"`
		NewPassword string `json:"newPassword"`
	}
	if err := c.Bind(&input, e.MaxBodyBytes); err != nil || input.Username == "" || len(input.NewPassword) < minimumResetPasswordSize {
		return invalidRequest()
	}
	local, ok := e.Verifier.(LocalVerifier)
	if !ok || local.Store == nil || local.Passwords == nil {
		return notImplemented()
	}
	session, err := e.Sessions.Authorize(r)
	actor := "bootstrap"
	if err != nil {
		if e.BootstrapToken == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Bootstrap-Token")), []byte(e.BootstrapToken)) != 1 {
			return webkit.NewAPIError(http.StatusUnauthorized, "UNAUTHORIZED", map[string]any{"hint": "sign in to the console or present the X-Bootstrap-Token from the server log"})
		}
	} else if role := session.EffectiveRole(); role != rbac.RoleTenantAdmin && role != rbac.RoleSystemAdmin {
		return webkit.NewAPIError(http.StatusForbidden, "ROLE_FORBIDDEN", nil)
	} else {
		actor = session.AdminID
	}
	password := []byte(input.NewPassword)
	input.NewPassword = ""
	defer clear(password)
	hash, err := local.Passwords.Hash(password)
	if err != nil {
		return internalError()
	}
	if err := local.Store.ResetLocalPassword(c.Request().Context(), input.Username, hash); err != nil {
		return webkit.NewAPIError(http.StatusNotFound, "NOT_FOUND", nil)
	}
	// The old password could still present live sessions; void them so the
	// reset is immediately effective everywhere.
	if credential, lookupErr := local.Store.FindLocalCredential(c.Request().Context(), input.Username); lookupErr == nil {
		e.Sessions.InvalidateForAdmin(credential.AdminID)
	}
	if e.Audit != nil {
		if err := e.Audit(c.Request().Context(), actor, "local_password.emergency_reset", input.Username); err != nil {
			// The reset itself succeeded; surface the audit failure without
			// undoing it so operators still see the event gap.
			return internalError()
		}
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// changePassword lets a signed-in local account rotate its own password after
// proving the current one. It is distinct from the loopback-only emergency
// reset: it requires a valid session and the old password.
func (e SessionEndpoints) changePassword(c *webkit.Context) error {
	if e.MaxBodyBytes <= 0 {
		return invalidRequest()
	}
	session, err := e.Sessions.Authorize(c.Request())
	if err != nil {
		return webkit.NewAPIError(http.StatusUnauthorized, "UNAUTHORIZED", nil)
	}
	var input struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if err := c.Bind(&input, e.MaxBodyBytes); err != nil || input.OldPassword == "" || len(input.NewPassword) < minimumResetPasswordSize {
		return invalidRequest()
	}
	local, ok := e.Verifier.(LocalVerifier)
	if !ok || local.Store == nil || local.Passwords == nil {
		return notImplemented()
	}
	credential, err := local.Store.FindLocalUserByID(c.Request().Context(), session.AdminID)
	if err != nil {
		return webkit.NewAPIError(http.StatusUnauthorized, "UNAUTHORIZED", nil)
	}
	oldPassword := []byte(input.OldPassword)
	input.OldPassword = ""
	matched, err := local.Passwords.Verify(oldPassword, credential.PasswordHash)
	clear(oldPassword)
	if err != nil || !matched {
		return webkit.NewAPIError(http.StatusUnauthorized, "WRONG_PASSWORD", nil)
	}
	newPassword := []byte(input.NewPassword)
	input.NewPassword = ""
	defer clear(newPassword)
	hash, err := local.Passwords.Hash(newPassword)
	if err != nil {
		return internalError()
	}
	if err := local.Store.ResetLocalPassword(c.Request().Context(), credential.Username, hash); err != nil {
		return webkit.NewAPIError(http.StatusNotFound, "NOT_FOUND", nil)
	}
	// Force every session of the account (including this one) to re-authenticate
	// with the new password.
	e.Sessions.InvalidateForAdmin(session.AdminID)
	if e.Audit != nil {
		if auditErr := e.Audit(c.Request().Context(), session.AdminID, "auth.password_change", credential.Username); auditErr != nil {
			return internalError()
		}
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

func (e SessionEndpoints) login(c *webkit.Context) error {
	if e.MaxBodyBytes <= 0 {
		return invalidRequest()
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.Bind(&input, e.MaxBodyBytes); err != nil {
		return invalidRequest()
	}
	ip := ""
	if e.LoginLimiter != nil {
		ip = clientIP(c.Request().RemoteAddr)
	}
	rateLimited := func() error {
		return webkit.NewAPIError(http.StatusTooManyRequests, "RATE_LIMITED", map[string]any{"retryAfterSeconds": int(e.LoginLimiter.Window().Seconds())})
	}
	now := time.Now()
	if e.LoginLimiter != nil && !e.LoginLimiter.IPAllowed(ip, now) {
		if e.LoginLimiter.ReportBlock("ip:"+ip, now) {
			e.emitSecurityEvent(c, "admin.security.rate_limited", "login_ip_attempt_limit")
		}
		return rateLimited()
	}
	password := []byte(input.Password)
	input.Password = ""
	defer clear(password)
	session, err := e.Verifier.Verify(c.Request().Context(), input.Username, password)
	if err != nil {
		blocked := e.LoginLimiter != nil && e.LoginLimiter.AccountBlocked(ip, input.Username, time.Now())
		if blocked {
			if e.LoginLimiter.ReportBlock("account:"+ip+"|"+input.Username, time.Now()) {
				e.emitSecurityEvent(c, "admin.security.rate_limited", "login_account_failure_limit")
			}
		} else {
			e.emitSecurityEvent(c, "admin.security.login_failed", "invalid_credentials")
		}
		if e.Audit != nil {
			if auditErr := e.Audit(c.Request().Context(), "anonymous", "auth.login_failed", input.Username); auditErr != nil {
				return internalError()
			}
		}
		if blocked {
			return rateLimited()
		}
		return webkit.NewAPIError(http.StatusUnauthorized, "UNAUTHORIZED", nil)
	}
	if e.Audit != nil {
		if auditErr := e.Audit(c.Request().Context(), session.AdminID, "auth.login", session.Username); auditErr != nil {
			return internalError()
		}
	}
	csrf, err := e.Sessions.Create(c.Response(), session)
	if err != nil {
		return internalError()
	}
	return c.JSON(http.StatusOK, map[string]string{"csrfToken": csrf})
}

func (e SessionEndpoints) emitSecurityEvent(c *webkit.Context, kind, reason string) {
	if e.EventSink == nil || e.EventIDs == nil {
		return
	}
	id, err := e.EventIDs.New()
	if err != nil || id == "" {
		return
	}
	_ = e.EventSink.Emit(c.Request().Context(), contracts.DomainEvent{
		ID: id, Kind: kind, OccurredAt: time.Now(), Attributes: map[string]string{"reason": reason},
	})
}

func (e SessionEndpoints) logout(c *webkit.Context) error {
	if _, err := e.Sessions.Authorize(c.Request()); err != nil {
		return webkit.NewAPIError(http.StatusUnauthorized, "UNAUTHORIZED", nil)
	}
	e.Sessions.Destroy(c.Response(), c.Request())
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}
