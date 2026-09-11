package backend

import (
	"github.com/F31/liteAIG/internal/observability/audit"

	"time"
)

// DashboardView is the server-aggregated metrics payload (§37.49/50).
type DashboardView struct {
	Requests      int64   `json:"requests"`
	InputTokens   int64   `json:"inputTokens"`
	OutputTokens  int64   `json:"outputTokens"`
	AvgLatencyMS  int64   `json:"avgLatencyMS"`
	ConfigVersion int64   `json:"configVersion"`
	Spend         float64 `json:"spend,omitempty"`
}

// FinOpsView is the cost attribution and optimization payload.
type FinOpsView struct {
	Requests        int64   `json:"requests"`
	InputTokens     int64   `json:"inputTokens"`
	OutputTokens    int64   `json:"outputTokens"`
	Spend           float64 `json:"spend"`
	RetryCost       float64 `json:"retryCost"`
	FallbackCost    float64 `json:"fallbackCost"`
	CacheHits       int64   `json:"cacheHits"`
	SemanticHits    int64   `json:"semanticHits"`
	CacheHitRate    float64 `json:"cacheHitRate"`
	SemanticHitRate float64 `json:"semanticHitRate"`
	// SavedTokens is the would-be provider token load avoided by exact and
	// semantic cache hits in the ledger window.
	SavedTokens int64 `json:"savedTokens"`
	// CacheSavings is an avoidable-cost estimate (USD) from those saved tokens,
	// priced at a documented blended per-million-token rate. It is an estimate,
	// not a provider invoice.
	CacheSavings    float64                `json:"cacheSavings"`
	ByProject       []FinOpsProjectRow     `json:"byProject"`
	ByModel         []FinOpsModelRow       `json:"byModel"`
	Recommendations []FinOpsRecommendation `json:"recommendations"`
}

type RecommendationView struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"`
	Title       string         `json:"title"`
	Explanation string         `json:"explanation"`
	Change      map[string]any `json:"change"`
	Metric      string         `json:"metric"`
	Samples     int64          `json:"samples"`
	From        time.Time      `json:"from"`
	To          time.Time      `json:"to"`
	CreatedAt   time.Time      `json:"createdAt"`
	Acceptable  bool           `json:"acceptable"`
}

type HealthView struct {
	Providers []ProviderHealth `json:"providers"`
	Circuits  []CircuitHealth  `json:"circuits"`
	Ready     bool             `json:"ready"`
	Drift     string           `json:"drift"`
}

// AlertView is the alert inbox payload.
type AlertView struct {
	ID       string            `json:"id"`
	RuleID   string            `json:"ruleId"`
	Severity string            `json:"severity"`
	Status   string            `json:"status"`
	Message  string            `json:"message"`
	FiredAt  time.Time         `json:"firedAt"`
	Evidence map[string]string `json:"evidence"`
}

// RuleView is one typed alert rule.
type RuleView struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	RuleType  string  `json:"ruleType"`
	Metric    string  `json:"metric"`
	Operator  string  `json:"operator"`
	Threshold float64 `json:"threshold"`
	Severity  string  `json:"severity"`
	Enabled   bool    `json:"enabled"`
}

// NotificationTargetView is one severity-routed webhook delivery target.
type NotificationTargetView struct {
	URL         string `json:"url"`
	MinSeverity string `json:"minSeverity"` // low|medium|high|critical
}

// NotificationSettingsView is the tenant alert-notification configuration.
type NotificationSettingsView struct {
	WebhookURL      string                   `json:"webhookUrl"`
	Targets         []NotificationTargetView `json:"targets,omitempty"`
	DedupSeconds    int                      `json:"dedupSeconds,omitempty"`
	Enabled         bool                     `json:"enabled"`
	RequiresRestart bool                     `json:"requiresRestart"`
	UpdatedAt       time.Time                `json:"updatedAt,omitempty"`
}

type NotificationSettingsInput struct {
	WebhookURL   string                   `json:"webhookUrl"`
	Targets      []NotificationTargetView `json:"targets,omitempty"`
	DedupSeconds int                      `json:"dedupSeconds,omitempty"`
	Enabled      bool                     `json:"enabled"`
}

// LiveEvent is a summary-only request tail event (never a body).
type LiveEvent struct {
	RequestID    string `json:"requestId"`
	Outcome      string `json:"outcome"`
	DeploymentID string `json:"deploymentId"`
	LatencyMS    int64  `json:"latencyMS"`
}

// MeView is the backend-reported tenant identity; the handler enriches it
// with session-derived fields before serializing.
type MeView struct {
	TenantID string   `json:"tenantId"`
	Scopes   []string `json:"scopes"`
}

// AuditRecord aliases the observability domain type; the Console API returns
// audit trail entries verbatim.
type AuditRecord = audit.Record

type FinOpsProjectRow struct {
	ProjectID    string  `json:"projectId"`
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	Spend        float64 `json:"spend"`
}

type FinOpsModelRow struct {
	LogicalModel  string  `json:"logicalModel"`
	Requests      int64   `json:"requests"`
	InputTokens   int64   `json:"inputTokens"`
	OutputTokens  int64   `json:"outputTokens"`
	Spend         float64 `json:"spend"`
	RetryCount    int     `json:"retryCount"`
	FallbackCount int     `json:"fallbackCount"`
	SavedTokens   int64   `json:"savedTokens"`
}

type FinOpsRecommendation struct {
	Kind   string  `json:"kind"`
	Title  string  `json:"title"`
	Detail string  `json:"detail"`
	Impact float64 `json:"impact,omitempty"`
}

// HealthView is the health & circuits surface payload.
type ProviderHealth struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Healthy bool   `json:"healthy"`
	Reason  string `json:"reason"`
}

type CircuitHealth struct {
	DeploymentID string `json:"deploymentId"`
	CredentialID string `json:"credentialId"`
	State        string `json:"state"`
}
