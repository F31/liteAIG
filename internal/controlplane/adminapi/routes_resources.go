package adminapi

import (
	"net/http"
	"strings"

	"github.com/F31/liteAIG/internal/catalog"
	"github.com/F31/liteAIG/internal/controlplane/backend"
	"github.com/F31/liteAIG/internal/controlplane/rbac"
	"github.com/F31/liteAIG/internal/platform/webkit"
)

type ProviderResource = backend.ProviderResource
type CredentialResource = backend.CredentialResource
type DeploymentResource = backend.DeploymentResource
type LogicalModelResource = backend.LogicalModelResource
type RouteResource = backend.RouteResource
type KeyResource = backend.KeyResource
type MCPServerResource = backend.MCPServerResource
type ToolResource = backend.ToolResource
type DiscoveredTool = backend.DiscoveredTool
type AgentResource = backend.AgentResource
type CacheResource = backend.CacheResource
type SemanticCacheView = backend.SemanticCacheView
type BudgetResource = backend.BudgetResource
type RuntimeResources = backend.RuntimeResources

// Catalog views alias the catalog domain types; the Console API returns them
// verbatim.
type (
	ResourceCatalogView = catalog.ResourceCatalogView
	ProviderCatalogItem = catalog.ProviderCatalogItem
	ModelCatalogItem    = catalog.ModelCatalogItem
	RegionCatalogItem   = catalog.RegionCatalogItem
)

type GuardrailResource = backend.GuardrailResource
type GuardrailJudgeView = backend.GuardrailJudgeView
type GuardrailGroundingView = backend.GuardrailGroundingView

type ProjectCreateInput = backend.ProjectCreateInput

type ProjectSummary = backend.ProjectSummary

type TenantCreateInput = backend.TenantCreateInput

type TenantStatusInput = backend.TenantStatusInput

type TenantSummary = backend.TenantSummary

type CredentialOperator = backend.CredentialOperator

type CredentialCreateInput = backend.CredentialCreateInput

func (s *Server) tenants(c *webkit.Context) error {
	value, err := s.tenantSvc.Tenants(c.Request().Context())
	return jsonResult(c, value, err)
}

func (s *Server) createTenant(c *webkit.Context) error {
	var input TenantCreateInput
	if err := c.Bind(&input, 1<<20); err != nil || strings.TrimSpace(input.PublicRef) == "" || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.DefaultProjectName) == "" {
		return invalidRequest()
	}
	value, err := s.tenantSvc.CreateTenant(c.Request().Context(), input, sessionFrom(c).AdminID)
	if err != nil {
		return err
	}
	s.recordAudit(c, "tenant.create", "tenant", value.ID)
	return c.JSON(http.StatusOK, value)
}

func (s *Server) updateTenantStatus(c *webkit.Context) error {
	var input TenantStatusInput
	if err := c.Bind(&input, 1<<20); err != nil || (input.Status != "active" && input.Status != "suspended" && input.Status != "deleted") {
		return invalidRequest()
	}
	// Suspend/delete are high-risk tenant lifecycle mutations; require step-up
	// re-authentication with the acting account's current password.
	if input.Status == "suspended" || input.Status == "deleted" {
		if err := s.requireReauth(c); err != nil {
			return err
		}
	}
	value, err := s.tenantSvc.UpdateTenantStatus(c.Request().Context(), c.Param("id"), input, sessionFrom(c).AdminID)
	if err != nil {
		return err
	}
	s.recordAudit(c, "tenant.status.update", "tenant", value.ID)
	return c.JSON(http.StatusOK, value)
}

func (s *Server) switchTenant(c *webkit.Context) error {
	switcher, ok := s.authorizer.(tenantSessionSwitcher)
	if !ok {
		return notImplemented()
	}
	var input struct {
		TenantID string `json:"tenantId"`
	}
	if err := c.Bind(&input, 1<<20); err != nil || strings.TrimSpace(input.TenantID) == "" {
		return invalidRequest()
	}
	tenants, err := s.tenantSvc.Tenants(c.Request().Context())
	if err != nil {
		return err
	}
	for _, tenant := range tenants {
		if tenant.ID == input.TenantID && tenant.Status == "active" {
			session := sessionFrom(c)
			if session.EffectiveRole() != rbac.RoleSystemAdmin {
				if s.memberships == nil {
					return webkit.NewAPIError(http.StatusForbidden, "ROLE_FORBIDDEN", nil)
				}
				ok, err := s.memberships.HasActiveTenantMembership(c.Request().Context(), session.AdminID, tenant.ID)
				if err != nil {
					return err
				}
				if !ok {
					return webkit.NewAPIError(http.StatusForbidden, "ROLE_FORBIDDEN", nil)
				}
			}
			session, err := switcher.SwitchTenant(c.Request(), tenant.ID)
			if err != nil {
				return webkit.NewAPIError(http.StatusUnauthorized, "UNAUTHORIZED", nil)
			}
			s.recordAudit(c, "session.tenant.switch", "tenant", tenant.ID)
			return c.JSON(http.StatusOK, map[string]any{"tenantId": session.TenantID})
		}
	}
	return webkit.NewAPIError(http.StatusNotFound, "NOT_FOUND", nil)
}

func (s *Server) projects(c *webkit.Context) error {
	value, err := s.projectSvc.Projects(c.Request().Context(), scopeOf(c))
	return jsonResult(c, value, err)
}

func (s *Server) createProject(c *webkit.Context) error {
	var input ProjectCreateInput
	if err := c.Bind(&input, 1<<20); err != nil {
		return invalidRequest()
	}
	value, err := s.projectSvc.CreateProject(c.Request().Context(), scopeOf(c), input)
	if err != nil {
		return err
	}
	s.recordAudit(c, "project.create", "project", value.ID)
	return c.JSON(http.StatusOK, value)
}

func (s *Server) runtime(c *webkit.Context) error {
	value, err := s.runtimeSvc.Runtime(c.Request().Context(), scopeOf(c))
	return jsonResult(c, value, err)
}

func (s *Server) resourceCatalog(c *webkit.Context) error {
	value, err := s.runtimeSvc.ResourceCatalog(c.Request().Context(), scopeOf(c))
	return jsonResult(c, value, err)
}

func (s *Server) discoverMCPTools(c *webkit.Context) error {
	value, err := s.keySvc.DiscoverMCPTools(c.Request().Context(), scopeOf(c), c.Param("id"))
	return jsonResult(c, value, err)
}

func (s *Server) createCredential(c *webkit.Context) error {
	if s.credentials == nil {
		return notImplemented()
	}
	operator := s.credentials
	var input CredentialCreateInput
	if err := c.Bind(&input, 1<<20); err != nil || input.ProviderID == "" || input.Secret == "" {
		return invalidRequest()
	}
	defer clear([]byte(input.Secret))
	if err := operator.CreateCredential(c.Request().Context(), scopeOf(c), input, sessionFrom(c).AdminID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "created"})
}

func (s *Server) rotateCredential(c *webkit.Context) error {
	if s.credentials == nil {
		return notImplemented()
	}
	operator := s.credentials
	var input struct {
		Secret string `json:"secret"`
	}
	if err := c.Bind(&input, 1<<20); err != nil || input.Secret == "" {
		return invalidRequest()
	}
	secret := []byte(input.Secret)
	defer clear(secret)
	if err := operator.RotateCredential(c.Request().Context(), scopeOf(c), c.Param("id"), secret, sessionFrom(c).AdminID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "rotated"})
}

func (s *Server) disableCredential(c *webkit.Context) error {
	if s.credentials == nil {
		return notImplemented()
	}
	operator := s.credentials
	if err := operator.DisableCredential(c.Request().Context(), scopeOf(c), c.Param("id"), sessionFrom(c).AdminID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "disabled"})
}

func (s *Server) deleteCredential(c *webkit.Context) error {
	if s.credentials == nil {
		return notImplemented()
	}
	operator := s.credentials
	if err := operator.DeleteCredential(c.Request().Context(), scopeOf(c), c.Param("id"), sessionFrom(c).AdminID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}
