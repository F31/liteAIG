package backend

import (
	"github.com/F31/liteAIG/internal/tenancy"

	"context"
	guardraildomain "github.com/F31/liteAIG/internal/guardrail"
	"time"
)

type GuardrailPublisher interface {
	FastPublishGuardrail(context.Context, tenancy.TenantScope, guardraildomain.Policy, guardraildomain.ChangeType, string) (guardraildomain.Policy, error)
}

// SecurityEventView is the tenant-scoped security event list payload.
type SecurityEventView struct {
	ID       string    `json:"id"`
	RuleID   string    `json:"ruleId"`
	Action   string    `json:"action"`
	Occurred time.Time `json:"occurredAt"`
}

// ToolCallView is the tenant-scoped tool call list payload.
type ToolCallView struct {
	ID        string    `json:"id"`
	ToolName  string    `json:"toolName"`
	SessionID string    `json:"sessionId"`
	TaskID    string    `json:"taskId"`
	AgentID   string    `json:"agentId"`
	Occurred  time.Time `json:"occurredAt"`
}

// DelegationGrantView is an operator-visible agent delegation grant. It grants
// a bounded permission set from delegator agent to delegatee agent within one
// tenant; grant evaluation still intersects this set with runtime policies.
type DelegationGrantView struct {
	ID          string    `json:"id"`
	DelegatorID string    `json:"delegatorId"`
	DelegateeID string    `json:"delegateeId"`
	Permissions []string  `json:"permissions"`
	CreatedBy   string    `json:"createdBy"`
	CreatedAt   time.Time `json:"createdAt"`
}

type DelegationGrantInput struct {
	DelegatorID string   `json:"delegatorId"`
	DelegateeID string   `json:"delegateeId"`
	Permissions []string `json:"permissions"`
}

// SimulateRequest is a Routing Simulator replay input.
type SimulateRequest struct {
	Model     string                           `json:"model"`
	ProjectID string                           `json:"projectId"`
	Metrics   map[string]DeploymentMetricsView `json:"metrics,omitempty"`
}

// SimulateResult is the simulator's explainable decision.
type SimulateResult struct {
	Selected string             `json:"selected"`
	Fallback []string           `json:"fallback"`
	Evidence []SimulateEvidence `json:"evidence"`
}

// AgentGraphView is the read-only agent/task graph payload.
type AgentGraphView struct {
	RootTask  string              `json:"rootTask"`
	TotalCost float64             `json:"totalCost"`
	Hops      []AgentGraphHopView `json:"hops"`
}

// AgentGraphHopView is one per-hop row.
type AgentGraphHopView struct {
	Order   int     `json:"order"`
	AgentID string  `json:"agentId"`
	Model   string  `json:"model"`
	Cost    float64 `json:"cost"`
	Outcome string  `json:"outcome"`
}

// ApprovalView is the read-only approval inbox payload.
type ApprovalView struct {
	ID, Requester, Action, Target, Status string
	Approvers                             int
	DualApproval                          bool
}

// FederationView is the read-only federated agents surface payload.
type FederationView struct {
	Relationships  []RelationshipView  `json:"relationships"`
	ExternalAgents []ExternalAgentView `json:"externalAgents"`
	Procurement    []ProcurementView   `json:"procurement"`
	PushOutbox     A2APushOutboxView   `json:"pushOutbox"`
}

// A2APushOutboxView summarizes durable callback delivery state without
// exposing callback URLs, bearer tokens, or payload bodies.
type A2APushOutboxView struct {
	Pending               int        `json:"pending"`
	Sending               int        `json:"sending"`
	Delivered             int        `json:"delivered"`
	Failed                int        `json:"failed"`
	EarliestNextAttemptAt *time.Time `json:"earliestNextAttemptAt,omitempty"`
}

// PushDeliveryView is one sanitized failed callback delivery row for operator
// drill-down. It never includes the callback URL, bearer token, or payload body.
type PushDeliveryView struct {
	ID            string    `json:"id"`
	TaskID        string    `json:"taskId"`
	Status        string    `json:"status"`
	Attempts      int       `json:"attempts"`
	MaxAttempts   int       `json:"maxAttempts"`
	LastError     string    `json:"lastError"`
	NextAttemptAt time.Time `json:"nextAttemptAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// DeploymentMetricsView mirrors soft-scoring metrics for the simulator.
type DeploymentMetricsView struct {
	LatencyMS     float64 `json:"latencyMS"`
	Cost          float64 `json:"cost"`
	Load          float64 `json:"load"`
	CacheAffinity float64 `json:"cacheAffinity"`
}

// ExternalAgentView is one federated agent projection.
type ExternalAgentView struct {
	ID, Name, ExternalSubject, TrustBoundary, Status string
}

// ProcurementView is one relationship's external procurement summary.
type ProcurementView struct {
	RelationshipID string  `json:"relationshipId"`
	Limit          float64 `json:"limit"`
	Spent          float64 `json:"spent"`
}

// RelationshipView summarizes a federated trust relationship.
type RelationshipView struct {
	ID                string `json:"id"`
	ExternalAgentID   string `json:"externalAgentID"`
	Status            string `json:"status"`
	AssuranceLevel    string `json:"assuranceLevel"`
	DataBoundary      string `json:"dataBoundary"`
	HasVerifiedAnchor bool   `json:"hasVerifiedAnchor"`
	ProjectGrants     int    `json:"projectGrants"`
	CapabilityGrants  int    `json:"capabilityGrants"`
}

// VerificationKey is one PEM-encoded public key used to verify a signed Agent
// Card. Algorithm is a2a.AlgRS256 or a2a.AlgES256.
type VerificationKey struct {
	Alg string `json:"alg"`
	PEM string `json:"pem"`
}

// FederationDiscoverInput drives Agent Card discovery for a peer agent.
type FederationDiscoverInput struct {
	URL              string            `json:"url"`
	Name             string            `json:"name"`
	VerificationKeys []VerificationKey `json:"verificationKeys"`
	ProjectGrants    []string          `json:"projectGrants"`
	CapabilityGrants []string          `json:"capabilityGrants"`
}

// FederationReviewInput approves or rejects a discovered relationship. When
// approving, optional project/capability grants are merged so the relationship
// can activate.
type FederationReviewInput struct {
	Approved         bool     `json:"approved"`
	ProjectGrants    []string `json:"projectGrants"`
	CapabilityGrants []string `json:"capabilityGrants"`
}

// SimulateEvidence is one candidate's decision evidence.
type SimulateEvidence struct {
	DeploymentID string             `json:"deploymentId"`
	Eligible     bool               `json:"eligible"`
	Score        float64            `json:"score"`
	Breakdown    map[string]float64 `json:"breakdown"`
}
