package adminapi

import (
	"context"
	"net/http"
	"time"

	"github.com/F31/liteAIG/internal/platform/webkit"
)

// PasswordResetService is the business contract for the email-verified
// password reset flow. The app-layer implementation owns the one-time codes,
// the mail channel, and the account store; the presentation layer adds rate
// limiting and the HTTP contract only.
type PasswordResetService interface {
	// Enabled reports whether an email channel is configured.
	Enabled() bool
	// Forgot issues a one-time reset code to the account's email address. It
	// must not reveal whether the username exists or has an email set:
	// accounts without a reachable address are no-ops with a nil error.
	Forgot(ctx context.Context, username string) error
	// Confirm validates the one-time code and applies the new password.
	// Callers must map every failure to the same response so code guessing
	// cannot distinguish "wrong code" from "no such account".
	Confirm(ctx context.Context, username, code string, newPassword []byte) error
}

// Register mounts the forgot-password endpoints next to the session routes.
func (e SessionEndpoints) registerPasswordReset(engine *webkit.Engine) {
	engine.Handle("GET /api/admin/password/forgot/config", e.forgotConfig)
	engine.Handle("POST /api/admin/password/forgot", e.forgotPassword)
	engine.Handle("POST /api/admin/password/reset/confirm", e.confirmPasswordReset)
}

func (e SessionEndpoints) forgotConfig(c *webkit.Context) error {
	enabled := e.PasswordReset != nil && e.PasswordReset.Enabled()
	return c.JSON(http.StatusOK, map[string]bool{"enabled": enabled})
}

func (e SessionEndpoints) forgotPassword(c *webkit.Context) error {
	if e.MaxBodyBytes <= 0 {
		return invalidRequest()
	}
	var input struct {
		Username string `json:"username"`
	}
	if err := c.Bind(&input, e.MaxBodyBytes); err != nil || input.Username == "" || len([]rune(input.Username)) > 64 {
		return invalidRequest()
	}
	if e.ResetRateLimiter != nil && !e.ResetRateLimiter.AllowSend(clientIP(c.Request().RemoteAddr), input.Username, time.Now()) {
		return webkit.NewAPIError(http.StatusTooManyRequests, "RATE_LIMITED", map[string]any{"retryAfterSeconds": int(e.ResetRateLimiter.Window().Seconds())})
	}
	if e.PasswordReset == nil || !e.PasswordReset.Enabled() {
		// Uniform answer: never reveal that the channel is unconfigured or
		// that the account is unknown.
		return c.JSON(http.StatusAccepted, map[string]bool{"ok": true})
	}
	if err := e.PasswordReset.Forgot(c.Request().Context(), input.Username); err != nil {
		return webkit.NewAPIError(http.StatusBadGateway, "RESET_EMAIL_FAILED", nil)
	}
	return c.JSON(http.StatusAccepted, map[string]bool{"ok": true})
}

func (e SessionEndpoints) confirmPasswordReset(c *webkit.Context) error {
	if e.MaxBodyBytes <= 0 {
		return invalidRequest()
	}
	var input struct {
		Username    string `json:"username"`
		Code        string `json:"code"`
		NewPassword string `json:"newPassword"`
	}
	if err := c.Bind(&input, e.MaxBodyBytes); err != nil ||
		input.Username == "" || len([]rune(input.Username)) > 64 ||
		input.Code == "" || len(input.Code) > 16 ||
		len(input.NewPassword) < minimumResetPasswordSize {
		return invalidRequest()
	}
	password := []byte(input.NewPassword)
	input.NewPassword = ""
	defer clear(password)
	if e.PasswordReset == nil || !e.PasswordReset.Enabled() {
		return webkit.NewAPIError(http.StatusUnauthorized, "INVALID_CODE", nil)
	}
	if err := e.PasswordReset.Confirm(c.Request().Context(), input.Username, input.Code, password); err != nil {
		// Every failure looks identical: a wrong code, an expired code, and
		// an unknown account are indistinguishable to the caller.
		return webkit.NewAPIError(http.StatusUnauthorized, "INVALID_CODE", nil)
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}
