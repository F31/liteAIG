package config

import (
	"fmt"
	"time"

	"github.com/F31/liteAIG/internal/identity/apikey"
	"github.com/F31/liteAIG/internal/kernel/runtime"
)

func CompileGlobal(version int64, publishedAt time.Time) *runtime.GlobalRuntime {
	return runtime.NewGlobalRuntime(runtime.GlobalRuntimeData{Version: version, PublishedAt: publishedAt})
}

func CompileGlobalWithPeppers(version int64, publishedAt time.Time, peppers []apikey.PepperVersion) *runtime.GlobalRuntime {
	refs := make(map[int]string, len(peppers))
	for _, item := range peppers {
		refs[item.Version] = item.Ref
	}
	return runtime.NewGlobalRuntime(runtime.GlobalRuntimeData{Version: version, PublishedAt: publishedAt, PepperRefs: refs})
}

func Compile(document TenantConfig, version int64, publishedAt time.Time) (*runtime.TenantRuntimeSnapshot, error) {
	return CompileWithAPIKeys(document, version, publishedAt, nil)
}

func CompileWithSystemDefaults(system SystemConfig, document TenantConfig, version int64, publishedAt time.Time) (*runtime.TenantRuntimeSnapshot, error) {
	return CompileWithSystemDefaultsAndAPIKeys(system, document, version, publishedAt, nil)
}

func CompileWithAPIKeys(document TenantConfig, version int64, publishedAt time.Time, keys []apikey.Record) (*runtime.TenantRuntimeSnapshot, error) {
	return CompileWithSystemDefaultsAndAPIKeys(SystemConfig{}, document, version, publishedAt, keys)
}

func CompileWithSystemDefaultsAndAPIKeys(system SystemConfig, document TenantConfig, version int64, publishedAt time.Time, keys []apikey.Record) (*runtime.TenantRuntimeSnapshot, error) {
	document = ApplySystemDefaults(system, document)
	diagnostics := Validate(document)
	if HasErrors(diagnostics) {
		return nil, fmt.Errorf("config validation failed")
	}
	data := runtime.TenantSnapshotData{
		TenantID: document.Tenant.ID, TenantRef: document.Tenant.PublicRef, Status: document.Tenant.Status, Version: version,
		SecurityEpoch: document.Tenant.SecurityEpoch, PublishedAt: publishedAt,
	}
	projects := make(map[string]Project, len(document.Projects))
	for _, item := range document.Projects {
		inheritTenantProjectPolicy(document.Tenant, &item)
		projects[item.ID] = item
		data.Projects = append(data.Projects, runtime.Project{ID: item.ID, Status: item.Status, AllowedDataRegions: item.AllowedDataRegions, ResidencyEnforcement: item.ResidencyEnforcement})
	}
	for _, item := range document.Providers {
		data.Providers = append(data.Providers, runtime.Provider{ID: item.ID, OwnerScope: item.OwnerScope, Type: item.Type, Endpoint: item.Endpoint, Status: item.Status})
	}
	for _, item := range document.Credentials {
		data.Credentials = append(data.Credentials, runtime.Credential{ID: item.ID, ProviderID: item.ProviderID, OwnerScope: item.OwnerScope, SecretRef: item.SecretRef, Status: item.Status})
	}
	for _, item := range document.Deployments {
		data.Deployments = append(data.Deployments, runtime.Deployment{ID: item.ID, ProviderID: item.ProviderID, CredentialID: item.CredentialID, PoolID: item.PoolID, UpstreamModel: item.UpstreamModel, DataRegion: item.DataRegion, Status: item.Status, Capabilities: item.Capabilities, ContextWindow: item.ContextWindow, Priority: item.Priority})
		if item.LeaseCapacity > 0 {
			data.Leases = append(data.Leases, runtime.LeaseConfig{DeploymentID: item.ID, Capacity: item.LeaseCapacity, TTLSeconds: leaseTTLSeconds(item.LeaseTTLMS)})
		}
	}
	for _, item := range document.BudgetPolicies {
		data.BudgetPolicies = append(data.BudgetPolicies, runtime.BudgetPolicy{ID: item.ID, TenantID: item.TenantID, ProjectID: item.ProjectID, KeyID: item.KeyID, WindowHours: item.WindowHours, TokenLimit: item.TokenLimit, Mode: item.Mode, Consistency: item.Consistency})
	}
	for _, item := range document.CredentialPools {
		members := make([]runtime.PoolMember, 0, len(item.Members))
		for _, member := range item.Members {
			members = append(members, runtime.PoolMember{CredentialID: member.CredentialID, Weight: member.Weight})
		}
		data.CredentialPools = append(data.CredentialPools, runtime.CredentialPool{ID: item.ID, TenantID: item.TenantID, ProviderID: item.ProviderID, Strategy: item.Strategy, Members: members})
	}
	data.Circuit = runtime.CircuitConfig{MinSamples: document.Circuit.MinSamples, ErrorRate: document.Circuit.ErrorRate, InitialCooldownMS: document.Circuit.InitialCooldownMS, MaxCooldownMS: document.Circuit.MaxCooldownMS, CooldownFactor: document.Circuit.CooldownFactor}
	data.Drift = runtime.DriftMetadata{GraceMS: document.Drift.GraceMS, Strict: document.Drift.Strict, SecurityEpoch: document.Tenant.SecurityEpoch}
	data.Spool = runtime.SpoolPolicy{Mode: document.AccountingSpool.Mode, WarnThreshold: document.AccountingSpool.WarnThreshold, CriticalThreshold: document.AccountingSpool.CriticalThreshold, HardThreshold: document.AccountingSpool.HardThreshold, QuotaBytes: document.AccountingSpool.QuotaBytes}
	data.Cache = runtime.CachePolicy{Enabled: document.Cache.Enabled, TTLSeconds: document.Cache.TTLSeconds, NamespaceVersion: document.Cache.NamespaceVersion, AllowedKinds: document.Cache.AllowedKinds, MaxTemperature: document.Cache.MaxTemperature, Semantic: runtime.SemanticCachePolicy{Enabled: document.Cache.Semantic.Enabled, Model: document.Cache.Semantic.Model, Threshold: document.Cache.Semantic.Threshold}}
	for _, item := range document.MCPServers {
		data.MCPServers = append(data.MCPServers, runtime.MCPServer{ID: item.ID, URL: item.URL, Status: item.Status})
	}
	for _, item := range document.Tools {
		data.Tools = append(data.Tools, runtime.Tool{ID: item.ID, ServerID: item.ServerID, Name: item.Name, Schema: item.Schema, CapabilityTags: item.CapabilityTags, DataClassification: item.DataClassification, Endpoint: item.Endpoint, Status: item.Status})
	}
	for _, item := range document.ToolPolicies {
		data.ToolPolicies = append(data.ToolPolicies, runtime.ToolPolicy{ToolID: item.ToolID, ProjectID: item.ProjectID, AllowedAgentIDs: item.AllowedAgentIDs, Allowed: item.Allowed, RequireApproval: item.RequireApproval})
	}
	for _, item := range document.Agents {
		data.Agents = append(data.Agents, runtime.Agent{ID: item.ID, ProjectID: item.ProjectID, Name: item.Name, Status: item.Status, CurrentVersion: item.CurrentVersion, Capabilities: item.Capabilities, RequireApproval: effectiveAgentRequireApproval(projects, item)})
	}
	for _, item := range document.AgentEndpoints {
		data.AgentEndpoints = append(data.AgentEndpoints, runtime.AgentEndpoint{ID: item.ID, AgentID: item.AgentID, Version: item.Version, URL: item.URL, Protocol: item.Protocol, AuthScheme: item.AuthScheme, SecretRef: item.SecretRef, Headers: item.Headers, DataClassification: item.DataClassification, Capabilities: item.Capabilities})
	}
	for _, item := range document.FederatedAgents {
		data.FederatedAgents = append(data.FederatedAgents, runtime.FederatedAgent{ID: item.ID, Name: item.Name, ExternalSubject: item.ExternalSubject, TrustBoundary: item.TrustBoundary, Status: item.Status})
	}
	for _, item := range document.FederationRelationships {
		data.FederationRelationships = append(data.FederationRelationships, runtime.FederationRelationship{ID: item.ID, ExternalAgentID: item.ExternalAgentID, Status: item.Status, AssuranceLevel: item.AssuranceLevel, HasVerifiedAnchor: item.HasVerifiedAnchor, Direction: item.Direction, ProjectGrants: item.ProjectGrants, CapabilityGrants: item.CapabilityGrants, BoundaryStatus: item.BoundaryStatus, ProcessingRegions: item.ProcessingRegions, ApprovedVersion: item.ApprovedVersion})
	}
	rules := make([]runtime.GuardrailRule, 0, len(document.Guardrail.Rules))
	for _, item := range document.Guardrail.Rules {
		rules = append(rules, runtime.GuardrailRule{ID: item.ID, Kind: item.Kind, Pattern: item.Pattern, Action: item.Action, Replacement: item.Replacement})
	}
	data.Guardrail = runtime.GuardrailPolicy{
		Mode:          document.Guardrail.Mode,
		SecurityEpoch: document.Tenant.SecurityEpoch,
		Rules:         rules,
		Judge:         runtime.GuardrailJudge{Enabled: document.Guardrail.Judge.Enabled, Model: document.Guardrail.Judge.Model, Action: document.Guardrail.Judge.Action},
		Groundedness:  runtime.GuardrailGroundedness{Enabled: document.Guardrail.Groundedness.Enabled, MinOverlap: document.Guardrail.Groundedness.MinOverlap},
	}
	for _, item := range document.RoutePolicies {
		data.RoutePolicies = append(data.RoutePolicies, runtime.RoutePolicy{ID: item.ID, ProjectID: item.ProjectID, Strategy: item.Strategy, DeploymentIDs: item.DeploymentIDs, Weights: item.Weights, ScoreWeights: item.ScoreWeights, Version: item.Version})
	}
	for _, item := range document.LogicalModels {
		data.LogicalModels = append(data.LogicalModels, runtime.LogicalModel{ID: item.ID, Alias: item.Alias, RoutePolicyID: item.RoutePolicyID})
	}
	for _, item := range keys {
		if item.TenantID != document.Tenant.ID {
			return nil, fmt.Errorf("API key tenant scope mismatch")
		}
		projectExists := false
		for _, project := range document.Projects {
			if project.ID == item.ProjectID {
				projectExists = true
				break
			}
		}
		if !projectExists {
			return nil, fmt.Errorf("API key project scope mismatch")
		}
		data.APIKeys = append(data.APIKeys, runtime.APIKey{ID: item.ID, PublicID: item.PublicID, ProjectID: item.ProjectID, ApplicationID: item.ApplicationID, AgentID: item.AgentID, ServiceAccountID: item.ServiceAccountID, Status: item.Status, HMACDigest: item.HMACDigest, PepperVersion: item.PepperVersion, ExpiresAt: item.ExpiresAt, ModelAllowlist: item.ModelAllowlist, IPAllowlist: item.IPAllowlist})
	}
	return runtime.NewTenantSnapshot(data), nil
}

func ApplySystemDefaults(system SystemConfig, document TenantConfig) TenantConfig {
	if len(document.Tenant.AllowedDataRegions) == 0 && len(system.TenantDefaults.AllowedDataRegions) > 0 {
		document.Tenant.AllowedDataRegions = append([]string(nil), system.TenantDefaults.AllowedDataRegions...)
	}
	if document.Tenant.ResidencyEnforcement == "" {
		document.Tenant.ResidencyEnforcement = system.TenantDefaults.ResidencyEnforcement
	}
	return document
}

func inheritTenantProjectPolicy(tenant TenantResource, project *Project) {
	if len(project.AllowedDataRegions) == 0 && len(tenant.AllowedDataRegions) > 0 {
		project.AllowedDataRegions = append([]string(nil), tenant.AllowedDataRegions...)
	}
	if project.ResidencyEnforcement == "" {
		project.ResidencyEnforcement = tenant.ResidencyEnforcement
	}
}

func effectiveAgentRequireApproval(projects map[string]Project, agent Agent) bool {
	switch agent.ApprovalMode {
	case "inherit_project":
		project, ok := projects[agent.ProjectID]
		return ok && project.AgentDefaults.RequireApproval
	case "always":
		return true
	case "never":
		return false
	default:
		return agent.RequireApproval
	}
}

func leaseTTLSeconds(ms int64) int64 {
	if ms <= 0 {
		return 60
	}
	if ms < 1000 {
		return 1
	}
	return ms / 1000
}
