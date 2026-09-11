package adminapi

import (
	"github.com/F31/liteAIG/internal/controlplane/backend"
	"net/http"

	"github.com/F31/liteAIG/internal/controlplane/config"
	"github.com/F31/liteAIG/internal/platform/webkit"
)

type RebaseResult = backend.RebaseResult

type DraftSummary = backend.DraftSummary

func (s *Server) drafts(c *webkit.Context) error {
	value, err := s.configSvc.Drafts(c.Request().Context(), scopeOf(c))
	return jsonResult(c, value, err)
}

func (s *Server) createDraft(c *webkit.Context) error {
	value, err := s.configSvc.CreateDraft(c.Request().Context(), scopeOf(c), sessionFrom(c).AdminID)
	return jsonResult(c, value, err)
}

func (s *Server) getDraft(c *webkit.Context) error {
	value, err := s.configSvc.GetDraft(c.Request().Context(), scopeOf(c), c.Param("id"))
	return jsonResult(c, value, err)
}

func (s *Server) updateDraft(c *webkit.Context) error {
	var input struct {
		Revision int64               `json:"revision"`
		Config   config.TenantConfig `json:"config"`
	}
	if err := c.Bind(&input, 4<<20); err != nil {
		return invalidRequest()
	}
	value, err := s.configSvc.UpdateDraft(c.Request().Context(), scopeOf(c), c.Param("id"), input.Revision, input.Config, sessionFrom(c).AdminID)
	return jsonResult(c, value, err)
}

func (s *Server) diff(c *webkit.Context) error {
	value, err := s.configSvc.Diff(c.Request().Context(), scopeOf(c), c.Param("id"))
	return jsonResult(c, value, err)
}

func (s *Server) publish(c *webkit.Context) error {
	var input struct {
		Revision int64 `json:"revision"`
	}
	if err := c.Bind(&input, 0); err != nil {
		return invalidRequest()
	}
	value, diagnostics, err := s.configSvc.Publish(c.Request().Context(), scopeOf(c), c.Param("id"), input.Revision, sessionFrom(c).AdminID)
	if err != nil && len(diagnostics) > 0 {
		return webkit.NewAPIError(http.StatusUnprocessableEntity, "CONFIG_INVALID", map[string]any{"diagnostics": diagnostics})
	}
	return jsonResult(c, value, err)
}

func (s *Server) rollback(c *webkit.Context) error {
	var input struct {
		Version int64 `json:"version"`
	}
	if err := c.Bind(&input, 0); err != nil {
		return invalidRequest()
	}
	value, err := s.configSvc.Rollback(c.Request().Context(), scopeOf(c), input.Version, sessionFrom(c).AdminID)
	return jsonResult(c, value, err)
}

func (s *Server) versions(c *webkit.Context) error {
	value, err := s.configSvc.Versions(c.Request().Context(), scopeOf(c))
	return jsonResult(c, value, err)
}

func (s *Server) rebase(c *webkit.Context) error {
	var input struct {
		DraftID string `json:"draftId"`
	}
	if err := c.Bind(&input, 0); err != nil {
		return invalidRequest()
	}
	value, err := s.configSvc.Rebase(c.Request().Context(), scopeOf(c), input.DraftID, sessionFrom(c).AdminID)
	return jsonResult(c, value, err)
}
