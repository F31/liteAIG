package config

import (
	"fmt"
	"net/url"
	"slices"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Strategy and mode vocabularies are the single source of truth for
// validation. Runtime engines (routing planner, credential pool, guardrail)
// keep their own dispatch switches but must stay in sync with these sets.
var (
	routeStrategies   = []string{"priority", "weighted", "round_robin", "soft"}
	poolStrategies    = []string{"round_robin", "weighted", "least_inflight", "quota_aware"}
	budgetModes       = []string{"hard", "soft"}
	budgetConsistency = []string{"regional", "global_soft", "global_hard"}
	// federationRelationshipDirections is the direction vocabulary for a
	// federation relationship. Empty (legacy/undirected) is accepted.
	federationRelationshipDirections = []string{"", "outbound", "inbound", "bidirectional"}
	agentApprovalModes               = []string{"", "inherit_project", "always", "never"}
	// federationBoundaryStatuses is the data-boundary status vocabulary.
	federationBoundaryStatuses = []string{"", "unknown", "declared", "contractually_bound"}
)

type Diagnostic struct {
	Severity   Severity          `json:"severity"`
	Code       string            `json:"code"`
	Path       string            `json:"path"`
	MessageKey string            `json:"message_key"`
	Params     map[string]string `json:"params,omitempty"`
}

func Validate(document TenantConfig) []Diagnostic {
	return ValidateWithSystemDefaults(SystemConfig{}, document)
}

func ValidateWithSystemDefaults(system SystemConfig, document TenantConfig) []Diagnostic {
	document = ApplySystemDefaults(system, document)
	var out []Diagnostic
	add := func(code, path string, params map[string]string) {
		out = append(out, Diagnostic{Severity: SeverityError, Code: code, Path: path, MessageKey: "config.validation." + code, Params: params})
	}
	if document.SchemaVersion != SchemaV1 {
		add("unsupported_schema", "/schema_version", map[string]string{"supported": SchemaV1})
	}
	if document.Tenant.ID == "" || document.Tenant.PublicRef == "" {
		add("tenant_required", "/tenant", nil)
	}

	projects := make(map[string]Project)
	for i, item := range document.Projects {
		path := fmt.Sprintf("/projects/%d", i)
		if _, exists := projects[item.ID]; exists {
			add("duplicate_resource_id", path+"/id", map[string]string{"id": item.ID})
		}
		project := item
		inheritTenantProjectPolicy(document.Tenant, &project)
		projects[item.ID] = project
		if item.TenantID != document.Tenant.ID {
			add("tenant_scope_mismatch", path+"/tenant_id", nil)
		}
	}

	providers := make(map[string]Provider)
	for i, item := range document.Providers {
		path := fmt.Sprintf("/providers/%d", i)
		if _, exists := providers[item.ID]; exists {
			add("duplicate_resource_id", path+"/id", map[string]string{"id": item.ID})
		}
		providers[item.ID] = item
		validScope := item.OwnerScope == "SYSTEM_SHARED" && item.TenantID == "" ||
			item.OwnerScope == "TENANT_PRIVATE" && item.TenantID == document.Tenant.ID
		if !validScope {
			add("provider_scope_invalid", path+"/owner_scope", nil)
		}
	}

	credentials := make(map[string]Credential)
	for i, item := range document.Credentials {
		path := fmt.Sprintf("/credentials/%d", i)
		if _, exists := credentials[item.ID]; exists {
			add("duplicate_resource_id", path+"/id", map[string]string{"id": item.ID})
		}
		credentials[item.ID] = item
		provider, exists := providers[item.ProviderID]
		if !exists {
			add("provider_reference_missing", path+"/provider_id", nil)
		} else if item.OwnerScope != provider.OwnerScope || item.TenantID != provider.TenantID {
			add("credential_scope_invalid", path+"/owner_scope", nil)
		}
		if !validSecretRef(item.SecretRef) {
			add("secret_ref_invalid", path+"/secret_ref", nil)
		}
	}

	deployments := make(map[string]Deployment)
	for i, item := range document.Deployments {
		path := fmt.Sprintf("/deployments/%d", i)
		if _, exists := deployments[item.ID]; exists {
			add("duplicate_resource_id", path+"/id", map[string]string{"id": item.ID})
		}
		deployments[item.ID] = item
		if item.TenantID != document.Tenant.ID {
			add("tenant_scope_mismatch", path+"/tenant_id", nil)
		}
		if _, exists := providers[item.ProviderID]; !exists {
			add("provider_reference_missing", path+"/provider_id", nil)
		}
		credential, exists := credentials[item.CredentialID]
		if !exists || credential.ProviderID != item.ProviderID {
			add("credential_reference_invalid", path+"/credential_id", nil)
		}
	}

	routes := make(map[string]RoutePolicy)
	for i, item := range document.RoutePolicies {
		path := fmt.Sprintf("/route_policies/%d", i)
		if _, exists := routes[item.ID]; exists {
			add("duplicate_resource_id", path+"/id", map[string]string{"id": item.ID})
		}
		routes[item.ID] = item
		if item.TenantID != document.Tenant.ID {
			add("tenant_scope_mismatch", path+"/tenant_id", nil)
		}
		if item.ProjectID != "" {
			if _, exists := projects[item.ProjectID]; !exists {
				add("project_reference_missing", path+"/project_id", nil)
			}
		}
		if !slices.Contains(routeStrategies, item.Strategy) {
			add("route_strategy_invalid", path+"/strategy", nil)
		}
		eligible := 0
		for candidateIndex, id := range item.DeploymentIDs {
			deployment, exists := deployments[id]
			if !exists {
				add("deployment_reference_missing", fmt.Sprintf("%s/deployment_ids/%d", path, candidateIndex), nil)
				continue
			}
			if deployment.Status == "enabled" {
				eligible++
			}
			if project, exists := projects[item.ProjectID]; exists && project.ResidencyEnforcement == "strict" &&
				!slices.Contains(project.AllowedDataRegions, deployment.DataRegion) {
				add("data_residency_violation", fmt.Sprintf("%s/deployment_ids/%d", path, candidateIndex), nil)
			}
		}
		if eligible == 0 {
			add("route_has_no_eligible_deployment", path+"/deployment_ids", nil)
		}
	}

	logicalIDs, logicalAliases := map[string]bool{}, map[string]bool{}
	for i, item := range document.LogicalModels {
		path := fmt.Sprintf("/logical_models/%d", i)
		if logicalIDs[item.ID] || logicalAliases[item.Alias] {
			add("duplicate_resource_id", path+"/id", map[string]string{"id": item.ID})
		}
		logicalIDs[item.ID], logicalAliases[item.Alias] = true, true
		if item.TenantID != document.Tenant.ID {
			add("tenant_scope_mismatch", path+"/tenant_id", nil)
		}
		if _, exists := routes[item.RoutePolicyID]; !exists {
			add("route_reference_missing", path+"/route_policy_id", nil)
		}
	}
	if len(document.LogicalModels) == 0 {
		out = append(out, Diagnostic{Severity: SeverityWarning, Code: "no_logical_models", Path: "/logical_models", MessageKey: "config.validation.no_logical_models"})
	}

	budgetIDs := map[string]bool{}
	for i, item := range document.BudgetPolicies {
		path := fmt.Sprintf("/budget_policies/%d", i)
		if budgetIDs[item.ID] {
			add("duplicate_resource_id", path+"/id", map[string]string{"id": item.ID})
		}
		budgetIDs[item.ID] = true
		if item.TenantID != document.Tenant.ID {
			add("tenant_scope_mismatch", path+"/tenant_id", nil)
		}
		if item.WindowHours <= 0 || item.TokenLimit < 0 || !slices.Contains(budgetModes, item.Mode) {
			add("budget_policy_invalid", path, nil)
		}
		if item.Consistency != "" && !slices.Contains(budgetConsistency, item.Consistency) {
			add("budget_consistency_invalid", path+"/consistency", nil)
		}
		if item.ProjectID != "" {
			if _, exists := projects[item.ProjectID]; !exists {
				add("project_reference_missing", path+"/project_id", nil)
			}
		}
	}

	validateSpoolPolicy(document.AccountingSpool, add)
	validateCachePolicy(document.Cache, add)
	validateGuardrailPolicy(document.Guardrail, add)
	servers := map[string]bool{}
	for i, item := range document.MCPServers {
		path := fmt.Sprintf("/mcp_servers/%d", i)
		if item.TenantID != document.Tenant.ID {
			add("tenant_scope_mismatch", path+"/tenant_id", nil)
		}
		if item.ID == "" || item.URL == "" {
			add("mcp_server_invalid", path, nil)
		}
		servers[item.ID] = true
	}
	tools := map[string]bool{}
	for i, item := range document.Tools {
		path := fmt.Sprintf("/tools/%d", i)
		if item.TenantID != document.Tenant.ID {
			add("tenant_scope_mismatch", path+"/tenant_id", nil)
		}
		if item.ID == "" || item.Name == "" || !servers[item.ServerID] {
			add("tool_invalid", path, nil)
		}
		tools[item.ID] = true
	}
	for i, item := range document.ToolPolicies {
		path := fmt.Sprintf("/tool_policies/%d", i)
		if item.TenantID != document.Tenant.ID {
			add("tenant_scope_mismatch", path+"/tenant_id", nil)
		}
		if !tools[item.ToolID] {
			add("tool_reference_missing", path+"/tool_id", nil)
		}
		if _, ok := projects[item.ProjectID]; !ok {
			add("project_reference_missing", path+"/project_id", nil)
		}
	}
	for i, item := range document.Agents {
		path := fmt.Sprintf("/agents/%d", i)
		if item.TenantID != document.Tenant.ID {
			add("tenant_scope_mismatch", path+"/tenant_id", nil)
		}
		if item.ID == "" || item.Name == "" {
			add("agent_invalid", path, nil)
		}
		if _, ok := projects[item.ProjectID]; !ok {
			add("project_reference_missing", path+"/project_id", nil)
		}
		if !slices.Contains(agentApprovalModes, item.ApprovalMode) {
			add("agent_approval_mode_invalid", path+"/approval_mode", nil)
		}
	}

	validateAgentEndpoints(document, add)
	out = append(out, validateFederationRelationships(document)...)

	poolIDs := map[string]bool{}
	for i, item := range document.CredentialPools {
		path := fmt.Sprintf("/credential_pools/%d", i)
		if poolIDs[item.ID] {
			add("duplicate_resource_id", path+"/id", map[string]string{"id": item.ID})
		}
		poolIDs[item.ID] = true
		if item.TenantID != document.Tenant.ID {
			add("tenant_scope_mismatch", path+"/tenant_id", nil)
		}
		if !slices.Contains(poolStrategies, item.Strategy) {
			add("pool_strategy_invalid", path+"/strategy", nil)
		}
		provider, providerExists := providers[item.ProviderID]
		if !providerExists {
			add("provider_reference_missing", path+"/provider_id", nil)
		}
		for memberIndex, member := range item.Members {
			credential, exists := credentials[member.CredentialID]
			if !exists {
				add("credential_reference_missing", fmt.Sprintf("%s/members/%d", path, memberIndex), nil)
				continue
			}
			if providerExists && credential.ProviderID != provider.ID {
				add("pool_member_provider_mismatch", fmt.Sprintf("%s/members/%d", path, memberIndex), nil)
			}
			if member.Weight < 0 {
				add("pool_member_weight_invalid", fmt.Sprintf("%s/members/%d", path, memberIndex), nil)
			}
		}
		if len(item.Members) == 0 {
			add("pool_has_no_members", path+"/members", nil)
		}
	}

	for i, item := range document.Deployments {
		if item.PoolID == "" {
			continue
		}
		pool, exists := document.poolByID(item.PoolID)
		if !exists {
			add("pool_reference_missing", fmt.Sprintf("/deployments/%d/pool_id", i), nil)
			continue
		}
		if pool.TenantID != document.Tenant.ID {
			add("tenant_scope_mismatch", fmt.Sprintf("/deployments/%d/pool_id", i), nil)
		}
		if pool.ProviderID != item.ProviderID {
			add("deployment_pool_provider_mismatch", fmt.Sprintf("/deployments/%d/pool_id", i), nil)
		}
	}

	if document.Circuit.MinSamples > 0 {
		if document.Circuit.MinSamples < 1 || document.Circuit.ErrorRate <= 0 || document.Circuit.ErrorRate > 1 ||
			document.Circuit.InitialCooldownMS <= 0 || document.Circuit.MaxCooldownMS < document.Circuit.InitialCooldownMS {
			add("circuit_config_invalid", "/circuit", nil)
		}
	}
	if document.Drift.GraceMS < 0 {
		add("drift_config_invalid", "/drift/grace_ms", nil)
	}

	return out
}

func ValidateForTenant(document TenantConfig, tenantID string) []Diagnostic {
	return ValidateForTenantWithSystemDefaults(SystemConfig{}, document, tenantID)
}

func ValidateForTenantWithSystemDefaults(system SystemConfig, document TenantConfig, tenantID string) []Diagnostic {
	diagnostics := ValidateWithSystemDefaults(system, document)
	if document.Tenant.ID != tenantID {
		diagnostics = append(diagnostics, Diagnostic{
			Severity: SeverityError, Code: "tenant_scope_mismatch", Path: "/tenant/id",
			MessageKey: "config.validation.tenant_scope_mismatch",
		})
	}
	return diagnostics
}

func HasErrors(diagnostics []Diagnostic) bool {
	return slices.ContainsFunc(diagnostics, func(item Diagnostic) bool { return item.Severity == SeverityError })
}

func (d TenantConfig) poolByID(id string) (CredentialPool, bool) {
	for _, pool := range d.CredentialPools {
		if pool.ID == id {
			return pool, true
		}
	}
	return CredentialPool{}, false
}

func validSecretRef(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme != "" && parsed.User == nil && (parsed.Host != "" || parsed.Opaque != "" || parsed.Path != "")
}

// endpointAuthSchemes is the accepted outbound agent endpoint credential
// vocabulary. Empty means the endpoint is reached without credentials.
var endpointAuthSchemes = []string{"", "bearer", "x-api-key"}

// validateFederationRelationships keeps the federation governance-enum checks
// light: an unknown nonempty direction or boundary_status warns but never
// blocks a publish, because the outbound A2A gate fails closed on them at call
// time anyway. A config with an unknown value remains persistable.
func validateFederationRelationships(document TenantConfig) []Diagnostic {
	var out []Diagnostic
	warn := func(code, path string) {
		out = append(out, Diagnostic{Severity: SeverityWarning, Code: code, Path: path, MessageKey: "config.validation." + code})
	}
	for i, item := range document.FederationRelationships {
		path := fmt.Sprintf("/federation_relationships/%d", i)
		if !slices.Contains(federationRelationshipDirections, item.Direction) {
			warn("federation_direction_invalid", path+"/direction")
		}
		if !slices.Contains(federationBoundaryStatuses, item.BoundaryStatus) {
			warn("federation_boundary_status_invalid", path+"/boundary_status")
		}
	}
	return out
}

func validateAgentEndpoints(document TenantConfig, add func(code, path string, params map[string]string)) {
	for i, item := range document.AgentEndpoints {
		path := fmt.Sprintf("/agent_endpoints/%d", i)
		if item.TenantID != document.Tenant.ID {
			add("tenant_scope_mismatch", path+"/tenant_id", nil)
		}
		if item.ID == "" || item.AgentID == "" || item.URL == "" || item.Protocol == "" {
			add("agent_endpoint_invalid", path, nil)
		}
		if parsed, err := url.Parse(item.URL); err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			add("agent_endpoint_url_invalid", path+"/url", nil)
		}
		if item.SecretRef != "" && !validSecretRef(item.SecretRef) {
			add("secret_ref_invalid", path+"/secret_ref", nil)
		}
		if !slices.Contains(endpointAuthSchemes, item.AuthScheme) {
			add("agent_endpoint_auth_invalid", path+"/auth_scheme", nil)
		}
		if item.SecretRef != "" && item.AuthScheme == "" {
			add("agent_endpoint_auth_invalid", path+"/auth_scheme", nil)
		}
		if item.AuthScheme != "" && item.SecretRef == "" {
			add("agent_endpoint_auth_invalid", path+"/secret_ref", nil)
		}
	}
}

func validateSpoolPolicy(policy SpoolPolicy, add func(code, path string, params map[string]string)) {
	if policy.Mode == "" {
		return
	}
	if !slices.Contains(budgetModes, policy.Mode) {
		add("spool_policy_invalid", "/accounting_spool/mode", nil)
		return
	}
	if policy.WarnThreshold < 0 || policy.CriticalThreshold < 0 || policy.HardThreshold < 0 || policy.QuotaBytes < 0 {
		add("spool_policy_invalid", "/accounting_spool", nil)
		return
	}
	if policy.WarnThreshold > 0 && policy.CriticalThreshold > 0 && policy.WarnThreshold > policy.CriticalThreshold {
		add("spool_threshold_order", "/accounting_spool", nil)
	}
	if policy.CriticalThreshold > 0 && policy.HardThreshold > 0 && policy.CriticalThreshold > policy.HardThreshold {
		add("spool_threshold_order", "/accounting_spool", nil)
	}
}

func validateCachePolicy(policy CachePolicy, add func(code, path string, params map[string]string)) {
	if policy.Enabled {
		if policy.TTLSeconds < 0 || policy.NamespaceVersion < 0 || policy.MaxTemperature < 0 {
			add("cache_policy_invalid", "/cache", nil)
		}
	}
	if policy.Semantic.Enabled {
		// Semantic lookup serves from the exact store, so it needs the exact
		// cache policy on and an embedding model to call upstream.
		if !policy.Enabled || policy.Semantic.Model == "" || policy.Semantic.Threshold < 0 || policy.Semantic.Threshold > 1 {
			add("cache_policy_invalid", "/cache/semantic", nil)
		}
	}
}

func validateGuardrailPolicy(policy GuardrailPolicy, add func(code, path string, params map[string]string)) {
	if policy.Judge.Enabled {
		// The judge is an LLM call, so it needs a model to route to and a
		// valid failure action (block drops the response, mark only records).
		if policy.Judge.Model == "" || (policy.Judge.Action != "" && policy.Judge.Action != "block" && policy.Judge.Action != "mark") {
			add("guardrail_policy_invalid", "/guardrail/judge", nil)
		}
	}
	if policy.Groundedness.Enabled && (policy.Groundedness.MinOverlap <= 0 || policy.Groundedness.MinOverlap > 1) {
		add("guardrail_policy_invalid", "/guardrail/groundedness", nil)
	}
}
