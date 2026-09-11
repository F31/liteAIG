package adminapi

import (
	"net/http"

	"github.com/F31/liteAIG/internal/identity/apikey"
	"github.com/F31/liteAIG/internal/platform/webkit"
)

func (s *Server) createKey(c *webkit.Context) error {
	var input apikey.CreateInput
	if err := c.Bind(&input, 1<<20); err != nil {
		return invalidRequest()
	}
	value, err := s.keySvc.CreateKey(c.Request().Context(), scopeOf(c), input, sessionFrom(c).AdminID)
	if err != nil {
		return err
	}
	s.recordAudit(c, "api_key.create", "api_key", value.Record.ID)
	return c.JSON(http.StatusOK, value)
}

func (s *Server) listKeys(c *webkit.Context) error {
	value, err := s.keySvc.ListKeys(c.Request().Context(), scopeOf(c))
	return jsonResult(c, value, err)
}

func (s *Server) revealKey(c *webkit.Context) error {
	key, err := s.keySvc.RevealKey(c.Request().Context(), scopeOf(c), c.Param("id"))
	if err != nil {
		return err
	}
	s.recordAudit(c, "api_key.reveal", "api_key", c.Param("id"))
	return c.JSON(http.StatusOK, map[string]string{"key": key})
}

func (s *Server) revokeKey(c *webkit.Context) error {
	if err := s.keySvc.RevokeKey(c.Request().Context(), scopeOf(c), c.Param("id"), sessionFrom(c).AdminID); err != nil {
		return err
	}
	s.recordAudit(c, "api_key.revoke", "api_key", c.Param("id"))
	return c.JSON(http.StatusOK, map[string]string{"status": "revoked"})
}
