package config

import (
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/identity/apikey"
)

func TestCompileBuildsImmutableIndexes(t *testing.T) {
	document := validConfig()
	snapshot, err := Compile(document, 3, time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	document.RoutePolicies[0].DeploymentIDs[0] = "mutated"
	route, ok := snapshot.RoutePolicy("route-1")
	if !ok || route.DeploymentIDs[0] != "deployment-1" {
		t.Fatalf("compiled route = %+v", route)
	}
	route.DeploymentIDs[0] = "caller-mutated"
	fresh, _ := snapshot.RoutePolicy("route-1")
	if fresh.DeploymentIDs[0] != "deployment-1" {
		t.Fatalf("snapshot index was mutable: %+v", fresh)
	}
}

func TestCompileGlobalRuntime(t *testing.T) {
	publishedAt := time.Unix(20, 0)
	global := CompileGlobal(4, publishedAt)
	if global.Version != 4 || !global.PublishedAt.Equal(publishedAt) {
		t.Fatalf("global runtime = %+v", global)
	}
}

func TestCompileRuntimeKeyAndPepperIndexes(t *testing.T) {
	document := validConfig()
	digest := []byte{1, 2, 3}
	keys := []apikey.Record{{ID: "key", PublicID: "public", TenantID: "tenant-1", ProjectID: "project-1", Status: "active", HMACDigest: digest, PepperVersion: 4}}
	snapshot, err := CompileWithAPIKeys(document, 1, time.Unix(1, 0), keys)
	if err != nil {
		t.Fatal(err)
	}
	digest[0] = 9
	key, ok := snapshot.APIKey("public")
	if !ok || key.HMACDigest[0] != 1 {
		t.Fatalf("runtime key = %+v", key)
	}
	global := CompileGlobalWithPeppers(1, time.Unix(1, 0), []apikey.PepperVersion{{Version: 4, Ref: "secret://pepper/4"}})
	ref, ok := global.PepperRef(4)
	if !ok || ref != "secret://pepper/4" {
		t.Fatalf("pepper ref = %q, %t", ref, ok)
	}
}

func TestCompileSpoolPolicyIntoSnapshot(t *testing.T) {
	document := validConfig()
	document.AccountingSpool = SpoolPolicy{Mode: "hard", WarnThreshold: 0.7, CriticalThreshold: 0.9, HardThreshold: 1.0, QuotaBytes: 4096}
	snapshot, err := Compile(document, 1, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	policy := snapshot.SpoolPolicy()
	if policy.Mode != "hard" || policy.WarnThreshold != 0.7 || policy.CriticalThreshold != 0.9 || policy.HardThreshold != 1.0 || policy.QuotaBytes != 4096 {
		t.Fatalf("compiled spool policy = %+v", policy)
	}
}

func TestCompileCachePolicyIntoSnapshot(t *testing.T) {
	document := validConfig()
	document.Cache = CachePolicy{Enabled: true, TTLSeconds: 60, NamespaceVersion: 3, AllowedKinds: []string{"chat"}, MaxTemperature: 0.8}
	snapshot, err := Compile(document, 1, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	policy := snapshot.CachePolicy()
	if !policy.Enabled || policy.TTLSeconds != 60 || policy.NamespaceVersion != 3 || policy.MaxTemperature != 0.8 || len(policy.AllowedKinds) != 1 {
		t.Fatalf("compiled cache policy = %+v", policy)
	}
}

func TestCompileProjectInheritsTenantPolicyDefaults(t *testing.T) {
	document := validConfig()
	document.Tenant.AllowedDataRegions = []string{"eu"}
	document.Tenant.ResidencyEnforcement = "strict"
	document.Projects[0].AllowedDataRegions = nil
	document.Projects[0].ResidencyEnforcement = ""
	document.Deployments[0].DataRegion = "eu"
	snapshot, err := Compile(document, 1, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	project, ok := snapshot.Project("project-1")
	if !ok || project.ResidencyEnforcement != "strict" || len(project.AllowedDataRegions) != 1 || project.AllowedDataRegions[0] != "eu" {
		t.Fatalf("project=%+v", project)
	}

	document.Tenant.AllowedDataRegions[0] = "mutated"
	fresh, _ := snapshot.Project("project-1")
	if fresh.AllowedDataRegions[0] != "eu" {
		t.Fatalf("tenant default slice was not copied: %+v", fresh)
	}
}

func TestCompileTenantInheritsSystemPolicyDefaults(t *testing.T) {
	document := validConfig()
	document.Tenant.AllowedDataRegions = nil
	document.Tenant.ResidencyEnforcement = ""
	document.Projects[0].AllowedDataRegions = nil
	document.Projects[0].ResidencyEnforcement = ""
	document.Deployments[0].DataRegion = "eu"
	snapshot, err := CompileWithSystemDefaults(SystemConfig{TenantDefaults: TenantPolicyDefaults{AllowedDataRegions: []string{"eu"}, ResidencyEnforcement: "strict"}}, document, 1, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	project, ok := snapshot.Project("project-1")
	if !ok || project.ResidencyEnforcement != "strict" || len(project.AllowedDataRegions) != 1 || project.AllowedDataRegions[0] != "eu" {
		t.Fatalf("project=%+v", project)
	}

	document.Tenant.AllowedDataRegions = []string{"us"}
	document.Tenant.ResidencyEnforcement = "advisory"
	document.Deployments[0].DataRegion = "us"
	snapshot, err = CompileWithSystemDefaults(SystemConfig{TenantDefaults: TenantPolicyDefaults{AllowedDataRegions: []string{"eu"}, ResidencyEnforcement: "strict"}}, document, 1, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	project, _ = snapshot.Project("project-1")
	if project.ResidencyEnforcement != "advisory" || len(project.AllowedDataRegions) != 1 || project.AllowedDataRegions[0] != "us" {
		t.Fatalf("project=%+v", project)
	}
}

func TestCompileProjectPolicyOverridesTenantDefaults(t *testing.T) {
	document := validConfig()
	document.Tenant.AllowedDataRegions = []string{"eu"}
	document.Tenant.ResidencyEnforcement = "strict"
	document.Projects[0].AllowedDataRegions = []string{"us"}
	document.Projects[0].ResidencyEnforcement = "advisory"
	snapshot, err := Compile(document, 1, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	project, _ := snapshot.Project("project-1")
	if project.ResidencyEnforcement != "advisory" || len(project.AllowedDataRegions) != 1 || project.AllowedDataRegions[0] != "us" {
		t.Fatalf("project=%+v", project)
	}
}

func TestCompileGuardrailJudgeIntoSnapshot(t *testing.T) {
	document := validConfig()
	document.Guardrail = GuardrailPolicy{
		Judge:        GuardrailJudge{Enabled: true, Model: "gpt-4o-mini", Action: "block"},
		Groundedness: GuardrailGroundedness{Enabled: true, MinOverlap: 0.3},
	}
	snapshot, err := Compile(document, 1, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	policy := snapshot.GuardrailPolicy()
	if !policy.Judge.Enabled || policy.Judge.Model != "gpt-4o-mini" || policy.Judge.Action != "block" {
		t.Fatalf("compiled judge = %+v", policy.Judge)
	}
	if !policy.Groundedness.Enabled || policy.Groundedness.MinOverlap != 0.3 {
		t.Fatalf("compiled groundedness = %+v", policy.Groundedness)
	}
}

func TestCompileAgentEndpointAuthFieldsIntoSnapshot(t *testing.T) {
	document := validConfig()
	document.Agents = []Agent{{ID: "agent", TenantID: "tenant-1", ProjectID: "project-1", Name: "Agent", Status: "active"}}
	document.AgentEndpoints = []AgentEndpoint{{
		ID: "ep", TenantID: "tenant-1", AgentID: "agent", Version: "1",
		URL: "https://agent.example", Protocol: "a2a",
		AuthScheme: "bearer", SecretRef: "secret://tenant/a2a",
		Headers: map[string]string{"X-A2A-Tenant": "acme"},
	}}
	snapshot, err := Compile(document, 1, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	endpoint, ok := snapshot.AgentEndpoint("ep")
	if !ok {
		t.Fatal("compiled endpoint missing")
	}
	if endpoint.AuthScheme != "bearer" || endpoint.SecretRef != "secret://tenant/a2a" || endpoint.Headers["X-A2A-Tenant"] != "acme" {
		t.Fatalf("compiled endpoint = %+v", endpoint)
	}
}

func TestCompileAgentInheritsProjectApprovalDefault(t *testing.T) {
	document := validConfig()
	document.Projects[0].AgentDefaults.RequireApproval = true
	document.Agents = []Agent{{ID: "agent", TenantID: "tenant-1", ProjectID: "project-1", Name: "Agent", Status: "active", ApprovalMode: "inherit_project"}}
	snapshot, err := Compile(document, 1, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	agent, ok := snapshot.Agent("agent")
	if !ok || !agent.RequireApproval {
		t.Fatalf("agent=%+v", agent)
	}
}

func TestCompileAgentApprovalModeOverridesProjectDefault(t *testing.T) {
	document := validConfig()
	document.Projects[0].AgentDefaults.RequireApproval = true
	document.Agents = []Agent{{ID: "agent", TenantID: "tenant-1", ProjectID: "project-1", Name: "Agent", Status: "active", ApprovalMode: "never"}}
	snapshot, err := Compile(document, 1, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	agent, ok := snapshot.Agent("agent")
	if !ok || agent.RequireApproval {
		t.Fatalf("agent=%+v", agent)
	}

	document.Agents[0].ApprovalMode = "always"
	snapshot, err = Compile(document, 1, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	agent, ok = snapshot.Agent("agent")
	if !ok || !agent.RequireApproval {
		t.Fatalf("agent=%+v", agent)
	}
}

func TestCompilePolicyInheritanceMatrix(t *testing.T) {
	document := validConfig()
	document.Tenant.AllowedDataRegions = nil
	document.Tenant.ResidencyEnforcement = ""
	document.Projects[0].AllowedDataRegions = []string{"us"}
	document.Projects[0].ResidencyEnforcement = "advisory"
	document.Projects[0].AgentDefaults.RequireApproval = true
	document.Projects = append(document.Projects, Project{ID: "project-2", TenantID: "tenant-1", Status: "active"})
	document.Deployments = append(document.Deployments, Deployment{ID: "deployment-2", TenantID: "tenant-1", ProviderID: "provider-1", CredentialID: "credential-1", UpstreamModel: "model", DataRegion: "eu", Status: "enabled"})
	document.RoutePolicies = append(document.RoutePolicies, RoutePolicy{ID: "route-2", TenantID: "tenant-1", ProjectID: "project-2", Strategy: "priority", DeploymentIDs: []string{"deployment-2"}, Version: 1})
	document.LogicalModels = append(document.LogicalModels, LogicalModel{ID: "logical-2", TenantID: "tenant-1", Alias: "eu-chat", RoutePolicyID: "route-2"})
	document.Agents = []Agent{
		{ID: "agent-inherit-true", TenantID: "tenant-1", ProjectID: "project-1", Name: "Inherit True", Status: "active", ApprovalMode: "inherit_project"},
		{ID: "agent-always", TenantID: "tenant-1", ProjectID: "project-2", Name: "Always", Status: "active", ApprovalMode: "always"},
		{ID: "agent-never", TenantID: "tenant-1", ProjectID: "project-1", Name: "Never", Status: "active", ApprovalMode: "never"},
		{ID: "agent-legacy", TenantID: "tenant-1", ProjectID: "project-2", Name: "Legacy", Status: "active", RequireApproval: true},
	}

	snapshot, err := CompileWithSystemDefaults(SystemConfig{TenantDefaults: TenantPolicyDefaults{AllowedDataRegions: []string{"eu"}, ResidencyEnforcement: "strict"}}, document, 1, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	project1, ok := snapshot.Project("project-1")
	if !ok || project1.ResidencyEnforcement != "advisory" || len(project1.AllowedDataRegions) != 1 || project1.AllowedDataRegions[0] != "us" {
		t.Fatalf("project-1 = %+v", project1)
	}
	project2, ok := snapshot.Project("project-2")
	if !ok || project2.ResidencyEnforcement != "strict" || len(project2.AllowedDataRegions) != 1 || project2.AllowedDataRegions[0] != "eu" {
		t.Fatalf("project-2 = %+v", project2)
	}
	for id, want := range map[string]bool{"agent-inherit-true": true, "agent-always": true, "agent-never": false, "agent-legacy": true} {
		agent, ok := snapshot.Agent(id)
		if !ok || agent.RequireApproval != want {
			t.Fatalf("agent %s = %+v, ok=%t", id, agent, ok)
		}
	}
}

func TestCompileFederationRelationshipGovernanceIntoSnapshot(t *testing.T) {
	document := validConfig()
	document.Agents = []Agent{{ID: "agent", TenantID: "tenant-1", ProjectID: "project-1", Name: "Agent", Status: "active"}}
	document.AgentEndpoints = []AgentEndpoint{{ID: "ep", TenantID: "tenant-1", AgentID: "agent", Version: "1", URL: "https://agent.example", Protocol: "a2a"}}
	document.FederatedAgents = []FederatedAgent{{ID: "ext", TenantID: "tenant-1", Name: "Ext", ExternalSubject: "ext.example", TrustBoundary: "external_federated", Status: "active"}}
	document.FederationRelationships = []FederationRelationship{{
		ID: "rel", TenantID: "tenant-1", ExternalAgentID: "ext", Status: "active",
		AssuranceLevel: "high", HasVerifiedAnchor: true, Direction: "outbound",
		ProjectGrants: []string{"project-1"}, CapabilityGrants: []string{"chat"},
		BoundaryStatus: "contractually_bound", ProcessingRegions: []string{"eu-central-1"}, ApprovedVersion: "1",
	}}
	snapshot, err := Compile(document, 1, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	rel, ok := snapshot.FederationRelationship("rel")
	if !ok {
		t.Fatal("compiled relationship missing")
	}
	if rel.Direction != "outbound" || len(rel.ProjectGrants) != 1 || rel.ProjectGrants[0] != "project-1" ||
		len(rel.CapabilityGrants) != 1 || rel.CapabilityGrants[0] != "chat" ||
		rel.BoundaryStatus != "contractually_bound" || len(rel.ProcessingRegions) != 1 || rel.ProcessingRegions[0] != "eu-central-1" ||
		rel.ApprovedVersion != "1" {
		t.Fatalf("compiled relationship = %+v", rel)
	}
}

func TestCompileToolRegistryAndPolicy(t *testing.T) {
	document := validConfig()
	document.MCPServers = []MCPServer{{ID: "server", TenantID: "tenant-1", URL: "https://tools.example", Status: "active"}}
	document.Tools = []Tool{{ID: "tool", TenantID: "tenant-1", ServerID: "server", Name: "invoice.read", Schema: []byte(`{"required":["id"]}`), Status: "active"}}
	document.ToolPolicies = []ToolPolicy{{ToolID: "tool", TenantID: "tenant-1", ProjectID: "project-1", AllowedAgentIDs: []string{"agent"}, Allowed: true}}
	document.Agents = []Agent{{ID: "agent", TenantID: "tenant-1", ProjectID: "project-1", Name: "Agent", Status: "active"}}
	snapshot, err := Compile(document, 1, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := snapshot.Tool("tool")
	if !ok || tool.Name != "invoice.read" {
		t.Fatalf("tool=%+v", tool)
	}
	policy, ok := snapshot.ToolPolicy("project-1", "tool")
	if !ok || !policy.Allowed || len(policy.AllowedAgentIDs) != 1 {
		t.Fatalf("policy=%+v", policy)
	}
	if agent, ok := snapshot.Agent("agent"); !ok || agent.Name != "Agent" {
		t.Fatalf("agent=%+v", agent)
	}
}
