// Package accounting owns immutable request and usage finalization facts.
package accounting

import (
	"context"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/tenancy"
	"sync"
	"time"
)

type Attempt struct {
	DeploymentID string `json:"deploymentId"`
	Outcome      string `json:"outcome"`
	Number       int    `json:"number"`
	Retryable    bool   `json:"retryable"`
}
type RouteEvidence struct {
	DeploymentID string   `json:"deploymentId"`
	Eligible     bool     `json:"eligible"`
	Exclusions   []string `json:"exclusions"`
}
type Facts struct {
	UsageEventID, RequestID, TenantID, ProjectID, KeyID, LogicalModel, DeploymentID, Outcome, UsageSource, GuardrailStatus string
	InputTokens, OutputTokens                                                                                              int64
	CacheReadTokens, CacheWriteTokens, CachedInputTokens, ReasoningTokens, ToolCalls                                       int64
	RetryCount, FallbackCount                                                                                              int
	LatencyMS                                                                                                              int64
	SnapshotVersion, SecurityEpoch                                                                                         int64
	ProviderCost                                                                                                           *float64
	ProviderCurrency                                                                                                       string
	ReceivedAt, CompletedAt                                                                                                time.Time
	Attempts                                                                                                               []Attempt
	RouteEvidence                                                                                                          []RouteEvidence
	BudgetPolicyID, ReservationID                                                                                          string
	ActualTokens                                                                                                           *int64
	UserID, OrgUnitID, OrgPathSnapshot, CostCenterID, AttributionTrust                                                     string
	SessionID, TaskID, RootTaskID, ParentTaskID, AgentID                                                                   string
	AgentVersion, AgentEndpointID                                                                                          string
	Estimated                                                                                                              bool
	EstimationMethod                                                                                                       string
}
type RequestRecord struct {
	RequestID        string          `json:"requestId"`
	TenantID         string          `json:"tenantId"`
	ProjectID        string          `json:"projectId"`
	KeyID            string          `json:"keyId"`
	LogicalModel     string          `json:"logicalModel"`
	DeploymentID     string          `json:"deploymentId"`
	Outcome          string          `json:"outcome"`
	InputTokens      int64           `json:"inputTokens"`
	OutputTokens     int64           `json:"outputTokens"`
	RetryCount       int             `json:"retryCount"`
	FallbackCount    int             `json:"fallbackCount"`
	RouteEvidence    []RouteEvidence `json:"routeEvidence"`
	Attempts         []Attempt       `json:"attempts"`
	GuardrailStatus  string          `json:"guardrailStatus"`
	ProviderCost     *float64        `json:"providerCost,omitempty"`
	ProviderCurrency string          `json:"providerCurrency,omitempty"`
	Source           string          `json:"source"`
	LatencyMS        int64           `json:"latencyMS"`
	SnapshotVersion  int64           `json:"snapshotVersion"`
	SecurityEpoch    int64           `json:"securityEpoch"`
	UserID           string          `json:"userId,omitempty"`
	OrgUnitID        string          `json:"orgUnitId,omitempty"`
	OrgPathSnapshot  string          `json:"orgPathSnapshot,omitempty"`
	CostCenterID     string          `json:"costCenterId,omitempty"`
	AttributionTrust string          `json:"attributionTrust"`
	SessionID        string          `json:"sessionId,omitempty"`
	TaskID           string          `json:"taskId,omitempty"`
	RootTaskID       string          `json:"rootTaskId,omitempty"`
	ParentTaskID     string          `json:"parentTaskId,omitempty"`
	AgentID          string          `json:"agentId,omitempty"`
	AgentVersion     string          `json:"agentVersion,omitempty"`
	AgentEndpointID  string          `json:"agentEndpointId,omitempty"`
}
type UsageRecord struct {
	RequestID, TenantID, ProjectID, LogicalModel, Status                             string
	InputTokens, OutputTokens, TotalTokens                                           int64
	CacheReadTokens, CacheWriteTokens, CachedInputTokens, ReasoningTokens, ToolCalls int64
	Cost                                                                             float64
	UserID, OrgUnitID, OrgPathSnapshot, CostCenterID                                 string
	AttributionTrust                                                                 string
	SessionID, TaskID, RootTaskID, ParentTaskID, AgentID                             string
	AgentVersion, AgentEndpointID                                                    string
}
type Repository interface {
	Finalize(context.Context, Facts) (bool, error)
	GetRequest(context.Context, tenancy.TenantScope, string) (*RequestRecord, error)
	ListRequests(context.Context, tenancy.TenantScope, int) ([]RequestRecord, error)
	GetUsage(context.Context, tenancy.TenantScope, string) (*UsageRecord, error)
}
type Budget interface {
	Reconcile(string, string, int64) error
	Release(string, string) error
}
type Result struct {
	Created        bool
	TelemetryError error
}
type Finalizer struct {
	repository Repository
	budget     Budget
	sink       contracts.EventSink
	facts      Facts
	timeout    time.Duration
	once       sync.Once
	result     Result
	err        error
}
