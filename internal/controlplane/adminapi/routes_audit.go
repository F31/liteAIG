package adminapi

import (
	"net/http"

	"github.com/F31/liteAIG/internal/controlplane/backend"
	"github.com/F31/liteAIG/internal/platform/webkit"
)

type AuditRetentionView = backend.AuditRetentionView

type AuditRetentionInput = backend.AuditRetentionInput

func (s *Server) auditRetention(c *webkit.Context) error {
	value, err := s.auditRetentionSvc.GetAuditRetention(c.Request().Context(), scopeOf(c))
	return jsonResult(c, value, err)
}

func (s *Server) setAuditRetention(c *webkit.Context) error {
	var input AuditRetentionInput
	if err := c.Bind(&input, 0); err != nil || input.RetentionDays < 0 || input.RetentionDays > 3650 {
		return invalidRequest()
	}
	value, err := s.auditRetentionSvc.SetAuditRetention(c.Request().Context(), scopeOf(c), input, sessionFrom(c).AdminID)
	if err != nil {
		return err
	}
	s.recordAudit(c, "audit.retention.update", "tenant", scopeOf(c).TenantID)
	return c.JSON(http.StatusOK, value)
}
