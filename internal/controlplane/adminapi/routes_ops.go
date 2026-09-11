package adminapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/F31/liteAIG/internal/controlplane/backend"
	"net/http"
	"strconv"
	"time"

	"github.com/F31/liteAIG/internal/controlplane/rbac"
	gatewayplayground "github.com/F31/liteAIG/internal/gateway/playground"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	kernelerrors "github.com/F31/liteAIG/internal/kernel/errors"
	"github.com/F31/liteAIG/internal/platform/webkit"
	routing "github.com/F31/liteAIG/internal/routing/model"
)

type DashboardView = backend.DashboardView

type FinOpsView = backend.FinOpsView

type FinOpsProjectRow = backend.FinOpsProjectRow

type FinOpsModelRow = backend.FinOpsModelRow

type FinOpsRecommendation = backend.FinOpsRecommendation

type RecommendationView = backend.RecommendationView

type ProviderHealth = backend.ProviderHealth
type CircuitHealth = backend.CircuitHealth
type HealthView = backend.HealthView

type AlertView = backend.AlertView

type RuleView = backend.RuleView

type NotificationSettingsView = backend.NotificationSettingsView

type NotificationSettingsInput = backend.NotificationSettingsInput

type LiveEvent = backend.LiveEvent

type AuditRecord = backend.AuditRecord

func (s *Server) dashboard(c *webkit.Context) error {
	value, err := s.dashboardSvc.Dashboard(c.Request().Context(), scopeOf(c))
	return jsonResult(c, value, err)
}

func (s *Server) finops(c *webkit.Context) error {
	value, err := s.dashboardSvc.FinOps(c.Request().Context(), scopeOf(c))
	return jsonResult(c, value, err)
}

func (s *Server) recommendations(c *webkit.Context) error {
	value, err := s.dashboardSvc.Recommendations(c.Request().Context(), scopeOf(c))
	return jsonResult(c, value, err)
}

func (s *Server) request(c *webkit.Context) error {
	value, err := s.requestSvc.Request(c.Request().Context(), scopeOf(c), c.Param("id"))
	return jsonResult(c, value, err)
}

func (s *Server) requests(c *webkit.Context) error {
	value, err := s.requestSvc.Requests(c.Request().Context(), scopeOf(c), 50)
	return jsonResult(c, value, err)
}

func (s *Server) usage(c *webkit.Context) error {
	value, err := s.requestSvc.Usage(c.Request().Context(), scopeOf(c), c.Param("id"))
	return jsonResult(c, value, err)
}

func (s *Server) audit(c *webkit.Context) error {
	limit := queryInt(c.Request(), "limit", 200)
	offset := queryInt(c.Request(), "offset", 0)
	value, err := s.requestSvc.Audit(c.Request().Context(), scopeOf(c), limit, offset)
	return jsonResult(c, value, err)
}

// evidence exports the tenant's read-only evidence archive. from/to are
// RFC3339 bounds; when omitted the full history is exported. The endpoint is
// gated by the evidence.export permission and never includes prompt/response
// bodies or secrets.
func (s *Server) evidence(c *webkit.Context) error {
	r := c.Request()
	from, err := parseEvidenceBound(r, "from")
	if err != nil {
		return err
	}
	to, err := parseEvidenceBound(r, "to")
	if err != nil {
		return err
	}
	if !from.IsZero() && !to.IsZero() && !from.Before(to) {
		return webkit.NewAPIError(http.StatusBadRequest, "INVALID_TIME_RANGE", nil)
	}
	if s.evidenceSvc == nil {
		return webkit.NewAPIError(http.StatusServiceUnavailable, "EVIDENCE_UNAVAILABLE", nil)
	}
	value, err := s.evidenceSvc.Evidence(r.Context(), scopeOf(c), from, to)
	return jsonResult(c, value, err)
}

// parseEvidenceBound parses an optional RFC3339 evidence query bound; a zero
// time is returned when the parameter is absent, meaning unbounded.
func parseEvidenceBound(r *http.Request, name string) (time.Time, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return time.Time{}, nil
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, webkit.NewAPIError(http.StatusBadRequest, "INVALID_TIME", map[string]any{"param": name})
	}
	return value, nil
}

// queryInt parses an optional integer query parameter with a fallback.
func queryInt(r *http.Request, name string, fallback int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

type MeView = backend.MeView

func (s *Server) me(c *webkit.Context) error {
	r := c.Request()
	session := sessionFrom(c)
	value, err := s.identitySvc.Me(r.Context(), scopeOf(c))
	if err != nil {
		return err
	}
	result := map[string]any{
		"tenantId": value.TenantID,
		"scopes":   value.Scopes,
	}
	role := session.EffectiveRole()
	result["role"] = role
	result["scopes"] = rbac.PermissionsFor(role)
	username := session.Username
	if username == "" {
		username = session.AdminID
	}
	result["username"] = username
	// Local accounts expose their reset-email address so the console can
	// prefill the account menu; IdP-only sessions simply omit it.
	if s.userStore != nil {
		if user, err := s.userStore.FindLocalUserByID(r.Context(), session.AdminID); err == nil {
			result["email"] = user.Email
		}
	}
	// Expose the session CSRF token so the SPA can restore it after a full
	// reload (the token is not persisted on the client).
	if manager, ok := s.authorizer.(interface {
		CSRFTokenFor(*http.Request) (string, error)
	}); ok {
		if token, tokenErr := manager.CSRFTokenFor(r); tokenErr == nil && token != "" {
			result["csrfToken"] = token
		}
	}
	return c.JSON(http.StatusOK, result)
}

func (s *Server) health(c *webkit.Context) error {
	value, err := s.dashboardSvc.Health(c.Request().Context(), scopeOf(c))
	return jsonResult(c, value, err)
}

func (s *Server) alerts(c *webkit.Context) error {
	value, err := s.alertSvc.Alerts(c.Request().Context(), scopeOf(c), c.Request().URL.Query().Get("status"))
	return jsonResult(c, value, err)
}

func (s *Server) alertRules(c *webkit.Context) error {
	value, err := s.alertSvc.AlertRules(c.Request().Context(), scopeOf(c))
	return jsonResult(c, value, err)
}

func (s *Server) createAlertRule(c *webkit.Context) error {
	var input json.RawMessage
	if err := c.Bind(&input, 1<<20); err != nil {
		return invalidRequest()
	}
	value, err := s.alertSvc.CreateAlertRule(c.Request().Context(), scopeOf(c), input, sessionFrom(c).AdminID)
	return jsonResult(c, value, err)
}

func (s *Server) importDefaultAlertRules(c *webkit.Context) error {
	value, err := s.alertSvc.ImportDefaultAlertRules(c.Request().Context(), scopeOf(c), sessionFrom(c).AdminID)
	return jsonResult(c, value, err)
}

func (s *Server) notificationSettings(c *webkit.Context) error {
	value, err := s.alertSvc.NotificationSettings(c.Request().Context(), scopeOf(c))
	return jsonResult(c, value, err)
}

func (s *Server) updateNotificationSettings(c *webkit.Context) error {
	var input NotificationSettingsInput
	if err := c.Bind(&input, 1<<20); err != nil {
		return invalidRequest()
	}
	value, err := s.alertSvc.UpdateNotificationSettings(c.Request().Context(), scopeOf(c), input, sessionFrom(c).AdminID)
	return jsonResult(c, value, err)
}

func (s *Server) deleteNotificationSettings(c *webkit.Context) error {
	if err := s.alertSvc.DeleteNotificationSettings(c.Request().Context(), scopeOf(c), sessionFrom(c).AdminID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) alertAction(c *webkit.Context) error {
	var input struct {
		Action string `json:"action"`
	}
	if err := c.Bind(&input, 0); err != nil {
		return invalidRequest()
	}
	if err := s.alertSvc.AlertAction(c.Request().Context(), scopeOf(c), c.Param("id"), input.Action, sessionFrom(c).AdminID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) resetCircuit(c *webkit.Context) error {
	var input struct {
		DeploymentID string `json:"deploymentId"`
		CredentialID string `json:"credentialId"`
	}
	if err := c.Bind(&input, 0); err != nil {
		return invalidRequest()
	}
	if err := s.alertSvc.ResetCircuit(c.Request().Context(), scopeOf(c), input.DeploymentID, input.CredentialID, sessionFrom(c).AdminID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

// liveTail streams request summaries as SSE (summary fields only, never a
// request body).
func (s *Server) liveTail(c *webkit.Context) error {
	r := c.Request()
	events, err := s.dashboardSvc.LiveTail(r.Context(), scopeOf(c))
	if err != nil {
		return internalError()
	}
	w := c.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		return internalError()
	}
	_, _ = w.Write([]byte("retry: 3000\n\n"))
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return nil
		case event, open := <-events:
			if !open {
				return nil
			}
			payload, _ := json.Marshal(event)
			_, _ = fmt.Fprintf(w, "id: %s\nevent: request.summary\ndata: %s\n\n", event.RequestID, payload)
			flusher.Flush()
		}
	}
}

func (s *Server) governance(c *webkit.Context) error {
	resources, err := s.runtimeSvc.Runtime(c.Request().Context(), scopeOf(c))
	return jsonResult(c, resources, err)
}

func (s *Server) playground(c *webkit.Context) error {
	var input PlaygroundRequest
	if err := c.Bind(&input, 4<<20); err != nil {
		return invalidRequest()
	}
	if input.Stream {
		return s.playgroundStream(c, input)
	}
	value, err := s.playgroundSvc.Playground(c.Request().Context(), scopeOf(c), input)
	return writePlaygroundResult(c, value, err)
}

func writePlaygroundResult(c *webkit.Context, value PlaygroundResponse, err error) error {
	if err != nil {
		status, code, params := playgroundError(err)
		return webkit.NewAPIError(status, code, params)
	}
	return c.JSON(http.StatusOK, value)
}

func playgroundError(err error) (int, string, map[string]any) {
	params := map[string]any{}
	var requestErr *gatewayplayground.RequestError
	if errors.As(err, &requestErr) && requestErr.RequestID != "" {
		params["requestId"] = requestErr.RequestID
	}
	var kernelErr *kernelerrors.Error
	if errors.As(err, &kernelErr) {
		params["message"] = kernelErr.Message
		return statusForKernelCode(kernelErr.Code), kernelErr.Code, params
	}
	var upstream *contracts.UpstreamError
	if errors.As(err, &upstream) {
		params["message"] = upstream.Message
		if upstream.StatusCode > 0 {
			params["upstreamStatus"] = upstream.StatusCode
		}
		if upstream.StatusCode == http.StatusTooManyRequests {
			return http.StatusTooManyRequests, upstream.Code, params
		}
		return http.StatusBadGateway, upstream.Code, params
	}
	if errors.Is(err, routing.ErrNoEligibleDeployment) {
		params["message"] = "no eligible deployment"
		return http.StatusServiceUnavailable, "NO_ELIGIBLE_DEPLOYMENT", params
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		params["message"] = "request timed out"
		return http.StatusGatewayTimeout, "TIMEOUT", params
	}
	params["message"] = "internal error"
	return http.StatusInternalServerError, "INTERNAL_ERROR", params
}

func statusForKernelCode(code string) int {
	return kernelerrors.StatusForCode(code)
}

// playgroundStream serves the streaming playground as SSE: one `token` event
// per normalized chunk, then a final `done` event with the usage summary.
func (s *Server) playgroundStream(c *webkit.Context, input PlaygroundRequest) error {
	r := c.Request()
	if s.playgroundStreamer == nil {
		return webkit.NewAPIError(http.StatusNotImplemented, "STREAMING_NOT_WIRED", nil)
	}
	streamer := s.playgroundStreamer
	w := c.Response()
	flusher, ok := w.(http.Flusher)
	if !ok {
		return webkit.NewAPIError(http.StatusInternalServerError, "FLUSH_UNSUPPORTED", nil)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	result, err := streamer.PlaygroundStream(r.Context(), scopeOf(c), input, func(event StreamEventView) {
		data, _ := json.Marshal(event)
		_, _ = fmt.Fprintf(w, "event: token\ndata: %s\n\n", data)
		flusher.Flush()
	})
	if err != nil {
		payload, _ := json.Marshal(map[string]string{"error": err.Error()})
		_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", payload)
		flusher.Flush()
		return nil
	}
	final, _ := json.Marshal(result)
	_, _ = fmt.Fprintf(w, "event: done\ndata: %s\n\n", final)
	flusher.Flush()
	return nil
}
