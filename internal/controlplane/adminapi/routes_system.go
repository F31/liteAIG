package adminapi

import (
	"net/http"

	"github.com/F31/liteAIG/internal/controlplane/config"
	"github.com/F31/liteAIG/internal/platform/webkit"
)

// systemConfig returns the persisted singleton system configuration. The route
// is system_admin-only; an unwired service answers 501 NOT_IMPLEMENTED.
func (s *Server) systemConfig(c *webkit.Context) error {
	if s.systemConfigSvc == nil {
		return notImplemented()
	}
	value, err := s.systemConfigSvc.SystemConfig(c.Request().Context())
	return jsonResult(c, value, err)
}

// setSystemConfig upserts the singleton system configuration on behalf of the
// acting system admin.
func (s *Server) setSystemConfig(c *webkit.Context) error {
	if s.systemConfigSvc == nil {
		return notImplemented()
	}
	// Writing the system-wide policy defaults is a high-risk mutation shared by
	// every tenant; require step-up re-authentication with the acting password.
	if err := s.requireReauth(c); err != nil {
		return err
	}
	var document config.SystemConfig
	if err := c.Bind(&document, 1<<20); err != nil {
		return invalidRequest()
	}
	if err := config.ValidateSystemConfig(document); err != nil {
		return webkit.NewAPIError(http.StatusBadRequest, "INVALID_REQUEST", map[string]any{"diagnostic": err.Error()})
	}
	value, err := s.systemConfigSvc.SetSystemConfig(c.Request().Context(), document, sessionFrom(c).AdminID)
	if err == nil {
		s.recordSystemAudit(c, "system_config.update", "system_config", "1")
	}
	return jsonResult(c, value, err)
}
