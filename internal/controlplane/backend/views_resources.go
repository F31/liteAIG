package backend

import (
	"github.com/F31/liteAIG/internal/catalog"
	"github.com/F31/liteAIG/internal/tenancy"

	"context"
	"encoding/json"
	"time"
)

type KeyResource struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ProjectID   string    `json:"projectId,omitempty"`
	Fingerprint string    `json:"fingerprint"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	Revealable  bool      `json:"revealable"`
}

type TenantCreateInput struct {
	PublicRef            string   `json:"publicRef"`
	Name                 string   `json:"name"`
	SettlementCurrency   string   `json:"settlementCurrency"`
	DefaultProjectName   string   `json:"defaultProjectName"`
	ResidencyEnforcement string   `json:"residencyEnforcement"`
	AllowedDataRegions   []string `json:"allowedDataRegions"`
}

type TenantStatusInput struct {
	Status string `json:"status"`
}

type TenantSummary struct {
	ID                 string    `json:"id"`
	PublicRef          string    `json:"publicRef"`
	Name               string    `json:"name"`
	Status             string    `json:"status"`
	SettlementCurrency string    `json:"settlementCurrency"`
	DefaultProjectID   string    `json:"defaultProjectId,omitempty"`
	CreatedAt          time.Time `json:"createdAt"`
}

type AuditRetentionView struct {
	TenantID      string    `json:"tenantId"`
	RetentionDays int       `json:"retentionDays"`
	UpdatedBy     string    `json:"updatedBy,omitempty"`
	UpdatedAt     time.Time `json:"updatedAt,omitempty"`
}

type AuditRetentionInput struct {
	RetentionDays int `json:"retentionDays"`
}

type DiscoveredTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

type RuntimeResources struct {
	Providers     []ProviderResource     `json:"providers"`
	Credentials   []CredentialResource   `json:"credentials"`
	Deployments   []DeploymentResource   `json:"deployments"`
	LogicalModels []LogicalModelResource `json:"logicalModels"`
	Routes        []RouteResource        `json:"routes"`
	Keys          []KeyResource          `json:"keys"`
	MCPServers    []MCPServerResource    `json:"mcpServers"`
	Tools         []ToolResource         `json:"tools"`
	Agents        []AgentResource        `json:"agents"`
	Budgets       []BudgetResource       `json:"budgets"`
	Cache         CacheResource          `json:"cache"`
	Guardrail     GuardrailResource      `json:"guardrail"`
}

// ProjectCreateInput is the create-project payload.
type ProjectCreateInput struct {
	Name                 string   `json:"name"`
	ResidencyEnforcement string   `json:"residencyEnforcement"`
	AllowedDataRegions   []string `json:"allowedDataRegions"`
}

// ProjectSummary is the tenant-visible projection of a project.
type ProjectSummary struct {
	ID                   string   `json:"id"`
	TenantID             string   `json:"tenantId"`
	Name                 string   `json:"name"`
	Status               string   `json:"status"`
	ResidencyEnforcement string   `json:"residencyEnforcement"`
	AllowedDataRegions   []string `json:"allowedDataRegions"`
}

type CredentialOperator interface {
	CreateCredential(context.Context, tenancy.TenantScope, CredentialCreateInput, string) error
	RotateCredential(context.Context, tenancy.TenantScope, string, []byte, string) error
	DisableCredential(context.Context, tenancy.TenantScope, string, string) error
	DeleteCredential(context.Context, tenancy.TenantScope, string, string) error
}

type CredentialCreateInput struct {
	ProviderID string `json:"providerId"`
	Secret     string `json:"secret"`
	Status     string `json:"status"`
}

type AgentResource struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type BudgetResource struct {
	ID          string  `json:"id"`
	ProjectID   string  `json:"projectId,omitempty"`
	KeyID       string  `json:"keyId,omitempty"`
	WindowHours int64   `json:"windowHours"`
	TokenLimit  float64 `json:"tokenLimit"`
	Mode        string  `json:"mode"`
	Consistency string  `json:"consistency"`
}

type CacheResource struct {
	Enabled          bool              `json:"enabled"`
	TTLSeconds       int64             `json:"ttlSeconds"`
	NamespaceVersion int64             `json:"namespaceVersion"`
	Semantic         SemanticCacheView `json:"semantic,omitempty"`
}

type CredentialResource struct {
	ID          string `json:"id"`
	ProviderID  string `json:"providerId"`
	Fingerprint string `json:"fingerprint"`
	Status      string `json:"status"`
}

type DeploymentResource struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerId"`
	Model      string `json:"model"`
	Region     string `json:"region"`
	Status     string `json:"status"`
}

type GuardrailGroundingView struct {
	Enabled    bool    `json:"enabled"`
	MinOverlap float64 `json:"minOverlap,omitempty"`
}

type GuardrailJudgeView struct {
	Enabled bool   `json:"enabled"`
	Model   string `json:"model,omitempty"`
	Action  string `json:"action,omitempty"`
}

type GuardrailResource struct {
	Mode         string                 `json:"mode"`
	RuleCount    int                    `json:"ruleCount"`
	Judge        GuardrailJudgeView     `json:"judge"`
	Groundedness GuardrailGroundingView `json:"groundedness"`
}

type LogicalModelResource struct {
	ID            string `json:"id"`
	Alias         string `json:"alias"`
	RoutePolicyID string `json:"routePolicyId"`
}

type MCPServerResource struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Status string `json:"status"`
}

type ProviderResource struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

type RouteResource struct {
	ID       string `json:"id"`
	Strategy string `json:"strategy"`
	Version  int64  `json:"version"`
}

type SemanticCacheView struct {
	Enabled   bool    `json:"enabled"`
	Model     string  `json:"model,omitempty"`
	Threshold float64 `json:"threshold,omitempty"`
}

type ToolResource struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Status             string `json:"status"`
	DataClassification string `json:"dataClassification,omitempty"`
}

// ResourceCatalogView aliases the catalog domain view.
type ResourceCatalogView = catalog.ResourceCatalogView
