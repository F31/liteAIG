package adminapi

import (
	"github.com/F31/liteAIG/internal/controlplane/backend"
	"net/http"

	guardraildomain "github.com/F31/liteAIG/internal/guardrail"
	"github.com/F31/liteAIG/internal/guardrail/builtin"
	"github.com/F31/liteAIG/internal/platform/webkit"
)

type GuardrailPublisher = backend.GuardrailPublisher

type SecurityEventView = backend.SecurityEventView

type ToolCallView = backend.ToolCallView

type DelegationGrantView = backend.DelegationGrantView

type DelegationGrantInput = backend.DelegationGrantInput

type SimulateRequest = backend.SimulateRequest

type DeploymentMetricsView = backend.DeploymentMetricsView

type SimulateResult = backend.SimulateResult

type SimulateEvidence = backend.SimulateEvidence

type AgentGraphView = backend.AgentGraphView

type AgentGraphHopView = backend.AgentGraphHopView

type ApprovalView = backend.ApprovalView

type FederationView = backend.FederationView

type A2APushOutboxView = backend.A2APushOutboxView

type PushDeliveryView = backend.PushDeliveryView

type RelationshipView = backend.RelationshipView

type FederationDiscoverInput = backend.FederationDiscoverInput

type FederationReviewInput = backend.FederationReviewInput

type ExternalAgentView = backend.ExternalAgentView

type ProcurementView = backend.ProcurementView

func (s *Server) fastPublishGuardrail(c *webkit.Context) error {
	r := c.Request()
	// The re-auth step must prove the session's current password: guardrail
	// fast publishes rewrite the data-plane content policy and are a high-risk
	// action per the Admin Security contract.
	if err := s.requireReauth(c); err != nil {
		return err
	}
	session := sessionFrom(c)
	if s.guardrails == nil {
		return notImplemented()
	}
	publisher := s.guardrails
	var input struct {
		ID     string                     `json:"id"`
		Change guardraildomain.ChangeType `json:"change"`
		Rules  []builtin.Rule             `json:"rules"`
	}
	if err := c.Bind(&input, 0); err != nil {
		return invalidRequest()
	}
	policy, err := publisher.FastPublishGuardrail(r.Context(), scopeOf(c), guardraildomain.Policy{ID: input.ID, TenantID: scopeOf(c).TenantID, Rules: input.Rules}, input.Change, session.AdminID)
	if err != nil {
		return webkit.NewAPIError(http.StatusBadRequest, "GUARDRAIL_PUBLISH_FAILED", nil)
	}
	return c.JSON(http.StatusOK, policy)
}

func (s *Server) securityEvents(c *webkit.Context) error {
	items, err := s.securitySvc.SecurityEvents(c.Request().Context(), scopeOf(c), 50)
	if err != nil {
		return internalError()
	}
	return c.JSON(http.StatusOK, items)
}

func (s *Server) toolCalls(c *webkit.Context) error {
	items, err := s.requestSvc.ToolCalls(c.Request().Context(), scopeOf(c), 50)
	if err != nil {
		return internalError()
	}
	return c.JSON(http.StatusOK, items)
}

func (s *Server) simulate(c *webkit.Context) error {
	var input SimulateRequest
	if err := c.Bind(&input, 0); err != nil {
		return invalidRequest()
	}
	result, err := s.securitySvc.Simulate(c.Request().Context(), scopeOf(c), input)
	if err != nil {
		return internalError()
	}
	return c.JSON(http.StatusOK, result)
}

func (s *Server) delegations(c *webkit.Context) error {
	if s.delegationSvc == nil {
		return notImplemented()
	}
	items, err := s.delegationSvc.Delegations(c.Request().Context(), scopeOf(c))
	if err != nil {
		return err
	}
	if items == nil {
		items = []DelegationGrantView{}
	}
	return c.JSON(http.StatusOK, items)
}

func (s *Server) grantDelegation(c *webkit.Context) error {
	if s.delegationSvc == nil {
		return notImplemented()
	}
	var input DelegationGrantInput
	if err := c.Bind(&input, 1<<20); err != nil || input.DelegatorID == "" || input.DelegateeID == "" || len(input.Permissions) == 0 {
		return invalidRequest()
	}
	value, err := s.delegationSvc.GrantDelegation(c.Request().Context(), scopeOf(c), input, sessionFrom(c).AdminID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, value)
}

func (s *Server) revokeDelegation(c *webkit.Context) error {
	if s.delegationSvc == nil {
		return notImplemented()
	}
	if err := s.delegationSvc.RevokeDelegation(c.Request().Context(), scopeOf(c), c.Param("id"), sessionFrom(c).AdminID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) federation(c *webkit.Context) error {
	view, err := s.federationSvc.Federation(c.Request().Context(), scopeOf(c))
	if err != nil {
		return internalError()
	}
	return c.JSON(http.StatusOK, view)
}

func (s *Server) federationSuspend(c *webkit.Context) error {
	// The acting identity is always the authenticated session; client-supplied
	// actor fields are ignored so decisions cannot be attributed to someone else.
	if err := s.federationSvc.FederationSuspend(c.Request().Context(), scopeOf(c), c.Param("id"), sessionFrom(c).AdminID); err != nil {
		return webkit.NewAPIError(http.StatusBadRequest, "FEDERATION_SUSPEND_FAILED", nil)
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

// federationDiscover fetches and verifies a peer Agent Card and creates the
// federation relationship candidate. Discovery never implies trust: the
// relationship stays a candidate until an operator reviews it.
func (s *Server) federationDiscover(c *webkit.Context) error {
	var input backend.FederationDiscoverInput
	if err := c.Bind(&input, 0); err != nil {
		return invalidRequest()
	}
	if input.URL == "" {
		return webkit.NewAPIError(http.StatusBadRequest, "INVALID_REQUEST", map[string]any{"field": "url"})
	}
	view, err := s.federationSvc.FederationDiscover(c.Request().Context(), scopeOf(c), input)
	if err != nil {
		return webkit.NewAPIError(http.StatusBadRequest, "FEDERATION_DISCOVER_FAILED", nil)
	}
	return c.JSON(http.StatusOK, view)
}

// federationReview approves or rejects a discovered relationship. Approval
// requires a verified anchor plus project and capability grants.
func (s *Server) federationReview(c *webkit.Context) error {
	var input backend.FederationReviewInput
	if err := c.Bind(&input, 0); err != nil {
		return invalidRequest()
	}
	if input.Approved {
		if err := s.requireReauth(c); err != nil {
			return err
		}
	}
	if err := s.federationSvc.FederationReview(c.Request().Context(), scopeOf(c), c.Param("id"), input, sessionFrom(c).AdminID); err != nil {
		return webkit.NewAPIError(http.StatusBadRequest, "FEDERATION_REVIEW_FAILED", map[string]any{"reason": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) approvals(c *webkit.Context) error {
	items, err := s.federationSvc.Approvals(c.Request().Context(), scopeOf(c), 50)
	if err != nil {
		return internalError()
	}
	return c.JSON(http.StatusOK, items)
}

// pushOutboxDeliveries lists the tenant's most recent sanitized A2A push
// callback deliveries, optionally filtered by ?status=
// (pending/sending/delivered/failed). Callback URLs, bearer tokens, and payload
// bodies are never exposed.
func (s *Server) pushOutboxDeliveries(c *webkit.Context) error {
	items, err := s.federationSvc.PushDeliveries(c.Request().Context(), scopeOf(c), c.Request().URL.Query().Get("status"), 50)
	if err != nil {
		return internalError()
	}
	if items == nil {
		items = []PushDeliveryView{}
	}
	return c.JSON(http.StatusOK, items)
}

func (s *Server) approvalAction(c *webkit.Context) error {
	var input struct {
		Decision string `json:"decision"`
	}
	if err := c.Bind(&input, 0); err != nil || input.Decision == "" {
		return invalidRequest()
	}
	// The acting identity is always the authenticated session; a client-supplied
	// actor is a forgery vector and is never read from the request body.
	if err := s.federationSvc.ApprovalAction(c.Request().Context(), scopeOf(c), c.Param("id"), input.Decision, sessionFrom(c).AdminID); err != nil {
		return webkit.NewAPIError(http.StatusBadRequest, "APPROVAL_ACTION_FAILED", nil)
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) agentGraph(c *webkit.Context) error {
	view, err := s.federationSvc.AgentGraph(c.Request().Context(), scopeOf(c), c.Param("rootTaskId"))
	if err != nil {
		return internalError()
	}
	return c.JSON(http.StatusOK, view)
}
