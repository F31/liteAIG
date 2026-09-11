// Package config owns versioned Control Plane configuration documents.
package config

import (
	"encoding/json"
	"time"
)

const SchemaV1 = "v1"

type SystemConfig struct {
	TenantDefaults TenantPolicyDefaults `json:"tenant_defaults,omitempty"`
}

type TenantPolicyDefaults struct {
	AllowedDataRegions   []string `json:"allowed_data_regions,omitempty"`
	ResidencyEnforcement string   `json:"residency_enforcement,omitempty"`
}

type TenantConfig struct {
	SchemaVersion           string                   `json:"schema_version"`
	Tenant                  TenantResource           `json:"tenant"`
	Projects                []Project                `json:"projects"`
	Providers               []Provider               `json:"providers"`
	Credentials             []Credential             `json:"credentials"`
	Deployments             []Deployment             `json:"deployments"`
	RoutePolicies           []RoutePolicy            `json:"route_policies"`
	LogicalModels           []LogicalModel           `json:"logical_models"`
	BudgetPolicies          []BudgetPolicy           `json:"budget_policies,omitempty"`
	CredentialPools         []CredentialPool         `json:"credential_pools,omitempty"`
	Circuit                 CircuitConfig            `json:"circuit,omitempty"`
	Drift                   DriftMetadata            `json:"drift,omitempty"`
	AccountingSpool         SpoolPolicy              `json:"accounting_spool,omitempty"`
	Cache                   CachePolicy              `json:"cache,omitempty"`
	MCPServers              []MCPServer              `json:"mcp_servers,omitempty"`
	Tools                   []Tool                   `json:"tools,omitempty"`
	ToolPolicies            []ToolPolicy             `json:"tool_policies,omitempty"`
	Agents                  []Agent                  `json:"agents,omitempty"`
	AgentEndpoints          []AgentEndpoint          `json:"agent_endpoints,omitempty"`
	FederatedAgents         []FederatedAgent         `json:"federated_agents,omitempty"`
	FederationRelationships []FederationRelationship `json:"federation_relationships,omitempty"`
	Guardrail               GuardrailPolicy          `json:"guardrail,omitempty"`
}

// GuardrailRule is one rule of a published guardrail policy.
type GuardrailRule struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Pattern     string `json:"pattern"`
	Action      string `json:"action"`
	Replacement string `json:"replacement,omitempty"`
}

// GuardrailPolicy is the compiled guardrail policy for a Tenant.
type GuardrailPolicy struct {
	Mode         string                `json:"mode,omitempty"`
	Rules        []GuardrailRule       `json:"rules,omitempty"`
	Judge        GuardrailJudge        `json:"judge,omitempty"`
	Groundedness GuardrailGroundedness `json:"groundedness,omitempty"`
}

// GuardrailJudge enables the LLM-as-Judge output checkpoint: completions are
// scored by a judge model and the action applied on a failed rubric.
type GuardrailJudge struct {
	Enabled bool   `json:"enabled,omitempty"`
	Model   string `json:"model,omitempty"`
	Action  string `json:"action,omitempty"` // block|mark
}

// GuardrailGroundedness enables the deterministic groundedness checkpoint that
// flags responses drifting from the conversation context.
type GuardrailGroundedness struct {
	Enabled    bool    `json:"enabled,omitempty"`
	MinOverlap float64 `json:"min_overlap,omitempty"`
}

type Agent struct {
	ID              string   `json:"id"`
	TenantID        string   `json:"tenant_id"`
	ProjectID       string   `json:"project_id"`
	Name            string   `json:"name"`
	Status          string   `json:"status"`
	CurrentVersion  string   `json:"current_version,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
	RequireApproval bool     `json:"require_approval,omitempty"`
	ApprovalMode    string   `json:"approval_mode,omitempty"`
}

type ProjectAgentDefaults struct {
	RequireApproval bool `json:"require_approval,omitempty"`
}

// AgentEndpoint is one versioned endpoint of an agent.
type AgentEndpoint struct {
	ID                 string            `json:"id"`
	TenantID           string            `json:"tenant_id"`
	AgentID            string            `json:"agent_id"`
	Version            string            `json:"version"`
	URL                string            `json:"url"`
	Protocol           string            `json:"protocol"`
	AuthScheme         string            `json:"auth_scheme,omitempty"` // "" | bearer | x-api-key
	SecretRef          string            `json:"secret_ref,omitempty"`  // secret backing AuthScheme
	Headers            map[string]string `json:"headers,omitempty"`     // extra outbound headers
	DataClassification string            `json:"data_classification,omitempty"`
	Capabilities       []string          `json:"capabilities,omitempty"`
}

// FederatedAgent is a tenant-scoped projection of an external agent.
type FederatedAgent struct {
	ID              string `json:"id"`
	TenantID        string `json:"tenant_id"`
	Name            string `json:"name"`
	ExternalSubject string `json:"external_subject"`
	TrustBoundary   string `json:"trust_boundary"`
	Status          string `json:"status"`
}

// FederationRelationship summarizes a compiled federated trust relationship.
// The governance fields (direction, grants, data boundary, approved version)
// are the facts the outbound A2A gate evaluates at call time.
type FederationRelationship struct {
	ID                string   `json:"id"`
	TenantID          string   `json:"tenant_id"`
	ExternalAgentID   string   `json:"external_agent_id"`
	Status            string   `json:"status"`
	AssuranceLevel    string   `json:"assurance_level"`
	HasVerifiedAnchor bool     `json:"has_verified_anchor"`
	Direction         string   `json:"direction,omitempty"`
	ProjectGrants     []string `json:"project_grants,omitempty"`
	CapabilityGrants  []string `json:"capability_grants,omitempty"`
	BoundaryStatus    string   `json:"boundary_status,omitempty"`
	ProcessingRegions []string `json:"processing_regions,omitempty"`
	ApprovedVersion   string   `json:"approved_version,omitempty"`
}

type MCPServer struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	URL      string `json:"url"`
	Status   string `json:"status"`
}

type Tool struct {
	ID                 string          `json:"id"`
	TenantID           string          `json:"tenant_id"`
	ServerID           string          `json:"server_id"`
	Name               string          `json:"name"`
	DataClassification string          `json:"data_classification,omitempty"`
	Endpoint           string          `json:"endpoint,omitempty"`
	Status             string          `json:"status"`
	Schema             json.RawMessage `json:"schema,omitempty"`
	CapabilityTags     []string        `json:"capability_tags,omitempty"`
}

type ToolPolicy struct {
	ToolID          string   `json:"tool_id"`
	TenantID        string   `json:"tenant_id"`
	ProjectID       string   `json:"project_id"`
	AllowedAgentIDs []string `json:"allowed_agent_ids,omitempty"`
	Allowed         bool     `json:"allowed"`
	RequireApproval bool     `json:"require_approval,omitempty"`
}

// CachePolicy governs the Exact Cache for a Tenant.
type CachePolicy struct {
	Enabled          bool                `json:"enabled"`
	TTLSeconds       int64               `json:"ttl_seconds,omitempty"`
	NamespaceVersion int64               `json:"namespace_version,omitempty"`
	AllowedKinds     []string            `json:"allowed_kinds,omitempty"`
	MaxTemperature   float64             `json:"max_temperature,omitempty"`
	Semantic         SemanticCachePolicy `json:"semantic,omitempty"`
}

// SemanticCachePolicy enables similarity lookup after an exact-cache miss.
// Embedding calls go to the tenant's own provider using Model.
type SemanticCachePolicy struct {
	Enabled   bool    `json:"enabled,omitempty"`
	Model     string  `json:"model,omitempty"`
	Threshold float64 `json:"threshold,omitempty"`
}

// SpoolPolicy is the per-Tenant accounting durability policy.
type SpoolPolicy struct {
	Mode              string  `json:"mode"`
	WarnThreshold     float64 `json:"warn_threshold,omitempty"`
	CriticalThreshold float64 `json:"critical_threshold,omitempty"`
	HardThreshold     float64 `json:"hard_threshold,omitempty"`
	QuotaBytes        int64   `json:"quota_bytes,omitempty"`
}

type TenantResource struct {
	ID                   string   `json:"id"`
	PublicRef            string   `json:"public_ref"`
	Status               string   `json:"status"`
	SecurityEpoch        int64    `json:"security_epoch"`
	AllowedDataRegions   []string `json:"allowed_data_regions,omitempty"`
	ResidencyEnforcement string   `json:"residency_enforcement,omitempty"`
}

type Project struct {
	ID                   string               `json:"id"`
	TenantID             string               `json:"tenant_id"`
	Status               string               `json:"status"`
	AllowedDataRegions   []string             `json:"allowed_data_regions,omitempty"`
	ResidencyEnforcement string               `json:"residency_enforcement"`
	AgentDefaults        ProjectAgentDefaults `json:"agent_defaults,omitempty"`
}

type Provider struct {
	ID         string `json:"id"`
	TenantID   string `json:"tenant_id,omitempty"`
	OwnerScope string `json:"owner_scope"`
	Type       string `json:"type"`
	Endpoint   string `json:"endpoint,omitempty"`
	Status     string `json:"status"`
}

type Credential struct {
	ID         string `json:"id"`
	ProviderID string `json:"provider_id"`
	TenantID   string `json:"tenant_id,omitempty"`
	OwnerScope string `json:"owner_scope"`
	SecretRef  string `json:"secret_ref"`
	Status     string `json:"status"`
}

type Deployment struct {
	ID            string   `json:"id"`
	TenantID      string   `json:"tenant_id"`
	ProviderID    string   `json:"provider_id"`
	CredentialID  string   `json:"credential_id,omitempty"`
	PoolID        string   `json:"pool_id,omitempty"`
	UpstreamModel string   `json:"upstream_model"`
	DataRegion    string   `json:"data_region"`
	Capabilities  []string `json:"capabilities,omitempty"`
	ContextWindow int      `json:"context_window,omitempty"`
	Priority      int      `json:"priority,omitempty"`
	LeaseCapacity int      `json:"lease_capacity,omitempty"`
	LeaseTTLMS    int64    `json:"lease_ttl_ms,omitempty"`
	Status        string   `json:"status"`
}

type RoutePolicy struct {
	ID            string             `json:"id"`
	TenantID      string             `json:"tenant_id"`
	ProjectID     string             `json:"project_id,omitempty"`
	Strategy      string             `json:"strategy"`
	DeploymentIDs []string           `json:"deployment_ids"`
	Weights       map[string]int     `json:"weights,omitempty"`
	ScoreWeights  map[string]float64 `json:"score_weights,omitempty"`
	Version       int64              `json:"version"`
}

type LogicalModel struct {
	ID            string `json:"id"`
	TenantID      string `json:"tenant_id"`
	Alias         string `json:"alias"`
	RoutePolicyID string `json:"route_policy_id"`
}

// BudgetPolicy is one compiled budget window scope.
type BudgetPolicy struct {
	ID          string  `json:"id"`
	TenantID    string  `json:"tenant_id"`
	ProjectID   string  `json:"project_id,omitempty"`
	KeyID       string  `json:"key_id,omitempty"`
	WindowHours int64   `json:"window_hours"`
	TokenLimit  float64 `json:"token_limit"`
	Mode        string  `json:"mode"`
	Consistency string  `json:"consistency,omitempty"`
}

// CredentialPool groups credentials with a selection strategy.
type CredentialPool struct {
	ID         string       `json:"id"`
	TenantID   string       `json:"tenant_id"`
	ProviderID string       `json:"provider_id"`
	Strategy   string       `json:"strategy"`
	Members    []PoolMember `json:"members"`
}

// PoolMember references one credential with an optional weight.
type PoolMember struct {
	CredentialID string `json:"credential_id"`
	Weight       int    `json:"weight,omitempty"`
}

// CircuitConfig is the full-circuit-state-machine configuration.
type CircuitConfig struct {
	MinSamples        int     `json:"min_samples,omitempty"`
	ErrorRate         float64 `json:"error_rate,omitempty"`
	InitialCooldownMS int64   `json:"initial_cooldown_ms,omitempty"`
	MaxCooldownMS     int64   `json:"max_cooldown_ms,omitempty"`
	CooldownFactor    float64 `json:"cooldown_factor,omitempty"`
}

// DriftMetadata carries drift expectations for readiness.
type DriftMetadata struct {
	GraceMS       int64 `json:"grace_ms,omitempty"`
	Strict        bool  `json:"strict,omitempty"`
	SecurityEpoch int64 `json:"security_epoch,omitempty"`
}

type Draft struct {
	ID          string
	TenantID    string
	BaseVersion int64
	Revision    int64
	Status      string
	Config      TenantConfig
	CreatedBy   string
	UpdatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Version struct {
	ID            string
	TenantID      string
	Version       int64
	SourceDraftID string
	Config        TenantConfig
	PublishedBy   string
	PublishedAt   time.Time
}
