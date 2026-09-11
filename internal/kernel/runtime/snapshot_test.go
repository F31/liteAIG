package runtime

import (
	"testing"
	"time"
)

type fakeRegistry struct {
	global *GlobalRuntime
	tenant *TenantRuntimeSnapshot
}

func (r fakeRegistry) Global() *GlobalRuntime { return r.global }

func (r fakeRegistry) Tenant(ref string) (*TenantRuntimeSnapshot, bool) {
	return r.tenant, r.tenant != nil && r.tenant.TenantRef == ref
}

func TestRegistryContractReturnsCapturedTenantView(t *testing.T) {
	snapshot := &TenantRuntimeSnapshot{TenantID: "tenant-1", TenantRef: "ref-1", Version: 3}
	var registry Registry = fakeRegistry{
		global: &GlobalRuntime{Version: 2},
		tenant: snapshot,
	}

	got, ok := registry.Tenant("ref-1")
	if !ok || got != snapshot {
		t.Fatalf("Tenant() = (%+v, %t), want captured snapshot", got, ok)
	}
	if _, ok := registry.Tenant("ref-2"); ok {
		t.Fatal("Tenant() resolved an unrelated tenant reference")
	}
}

func TestNewTenantSnapshotIsolatesSourceData(t *testing.T) {
	deployment := Deployment{ID: "d", ProviderID: "pv", Status: "enabled", Capabilities: []string{"chat"}}
	source := TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Status: "active", Version: 1,
		Projects:    []Project{{ID: "p", Status: "active", AllowedDataRegions: []string{"us"}}},
		Deployments: []Deployment{deployment},
		FederationRelationships: []FederationRelationship{{
			ID: "rel", ExternalAgentID: "ext", Status: "active", HasVerifiedAnchor: true,
			Direction: "outbound", ProjectGrants: []string{"p"}, CapabilityGrants: []string{"chat"},
			ProcessingRegions: []string{"eu-central-1"}, ApprovedVersion: "1",
		}},
	}
	snapshot := NewTenantSnapshot(source)

	deployment.Capabilities = append(deployment.Capabilities, "mutated")
	source.Projects[0].AllowedDataRegions = append(source.Projects[0].AllowedDataRegions, "eu")
	source.FederationRelationships[0].ProjectGrants = append(source.FederationRelationships[0].ProjectGrants, "mutated")
	source.FederationRelationships[0].CapabilityGrants = append(source.FederationRelationships[0].CapabilityGrants, "mutated")
	source.FederationRelationships[0].ProcessingRegions = append(source.FederationRelationships[0].ProcessingRegions, "mutated")

	got, ok := snapshot.Deployment("d")
	if !ok || len(got.Capabilities) != 1 || got.Capabilities[0] != "chat" {
		t.Fatalf("Deployment() = %+v, want source mutation isolated", got)
	}
	if project, _ := snapshot.Project("p"); len(project.AllowedDataRegions) != 1 {
		t.Fatalf("Project() = %+v, want source mutation isolated", project)
	}
	if rel, ok := snapshot.FederationRelationship("rel"); !ok ||
		len(rel.ProjectGrants) != 1 || rel.ProjectGrants[0] != "p" ||
		len(rel.CapabilityGrants) != 1 || rel.CapabilityGrants[0] != "chat" ||
		len(rel.ProcessingRegions) != 1 || rel.ProcessingRegions[0] != "eu-central-1" {
		t.Fatalf("FederationRelationship() = %+v, want source mutation isolated", rel)
	}
}

func TestSnapshotGettersReturnIsolatedCopies(t *testing.T) {
	expiry := time.Now().Add(time.Hour)
	snapshot := NewTenantSnapshot(TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Version: 1,
		Projects:        []Project{{ID: "p", Status: "active", AllowedDataRegions: []string{"us"}}},
		Deployments:     []Deployment{{ID: "d", ProviderID: "pv", CredentialID: "c", Status: "enabled", Capabilities: []string{"chat"}}},
		RoutePolicies:   []RoutePolicy{{ID: "r", ProjectID: "p", Strategy: "priority", DeploymentIDs: []string{"d"}, Weights: map[string]int{"d": 1}, ScoreWeights: map[string]float64{"d": 1}}},
		APIKeys:         []APIKey{{ID: "k", PublicID: "pub", ProjectID: "p", Status: "active", HMACDigest: []byte("sig"), ModelAllowlist: []string{"m"}, IPAllowlist: []string{"1.2.3.4"}, ExpiresAt: &expiry}},
		CredentialPools: []CredentialPool{{ID: "pool", ProviderID: "pv", TenantID: "tenant", Strategy: "round_robin", Members: []PoolMember{{CredentialID: "c", Weight: 1}}}},
		Tools:           []Tool{{ID: "tool", ServerID: "srv", Name: "tool", Status: "active", Schema: []byte(`{}`), CapabilityTags: []string{"chat"}}},
		ToolPolicies:    []ToolPolicy{{ToolID: "tool", ProjectID: "p", AllowedAgentIDs: []string{"a"}}},
		Agents:          []Agent{{ID: "a", ProjectID: "p", Name: "a", Status: "active", Capabilities: []string{"chat"}}},
		AgentEndpoints:  []AgentEndpoint{{ID: "e", AgentID: "a", Version: "1", URL: "u", Protocol: "http", AuthScheme: "bearer", SecretRef: "local://cred/e", Headers: map[string]string{"X-A2A-Tenant": "acme"}, Capabilities: []string{"chat"}}},
		FederationRelationships: []FederationRelationship{{
			ID: "rel", ExternalAgentID: "ext", Status: "active", AssuranceLevel: "high", HasVerifiedAnchor: true,
			Direction: "outbound", ProjectGrants: []string{"p"}, CapabilityGrants: []string{"chat"},
			BoundaryStatus: "declared", ProcessingRegions: []string{"eu-central-1"}, ApprovedVersion: "1",
		}},
		Guardrail: GuardrailPolicy{Mode: "shadow", Rules: []GuardrailRule{{ID: "g", Kind: "pattern"}}},
	})

	project, _ := snapshot.Project("p")
	project.AllowedDataRegions = append(project.AllowedDataRegions, "eu")
	if got, _ := snapshot.Project("p"); len(got.AllowedDataRegions) != 1 {
		t.Fatalf("Project() shared backing array: %+v", got)
	}

	deployment, _ := snapshot.Deployment("d")
	deployment.Capabilities = append(deployment.Capabilities, "vision")
	if got, _ := snapshot.Deployment("d"); len(got.Capabilities) != 1 {
		t.Fatalf("Deployment() shared backing array: %+v", got)
	}

	route, _ := snapshot.RoutePolicy("r")
	route.DeploymentIDs = append(route.DeploymentIDs, "x")
	route.Weights["x"] = 5
	route.ScoreWeights["x"] = 2
	if got, _ := snapshot.RoutePolicy("r"); len(got.DeploymentIDs) != 1 || got.Weights["x"] != 0 || got.ScoreWeights["x"] != 0 {
		t.Fatalf("RoutePolicy() shared backing storage: %+v", got)
	}

	key, _ := snapshot.APIKey("pub")
	key.HMACDigest = append(key.HMACDigest, 'x')
	key.ModelAllowlist = append(key.ModelAllowlist, "m2")
	key.IPAllowlist = append(key.IPAllowlist, "5.6.7.8")
	mutated := expiry.Add(time.Hour)
	key.ExpiresAt = &mutated
	if got, _ := snapshot.APIKey("pub"); len(got.HMACDigest) != 3 || len(got.ModelAllowlist) != 1 || len(got.IPAllowlist) != 1 || !got.ExpiresAt.Equal(expiry) {
		t.Fatalf("APIKey() shared backing storage: %+v", got)
	}

	pool, _ := snapshot.CredentialPool("pool")
	pool.Members = append(pool.Members, PoolMember{CredentialID: "c2"})
	if got, _ := snapshot.CredentialPool("pool"); len(got.Members) != 1 {
		t.Fatalf("CredentialPool() shared backing array: %+v", got)
	}

	tool, _ := snapshot.Tool("tool")
	tool.Schema = append(tool.Schema, 'x')
	tool.CapabilityTags = append(tool.CapabilityTags, "extra")
	if got, _ := snapshot.Tool("tool"); len(got.Schema) != 2 || len(got.CapabilityTags) != 1 {
		t.Fatalf("Tool() shared backing storage: %+v", got)
	}

	policy, _ := snapshot.ToolPolicy("p", "tool")
	policy.AllowedAgentIDs = append(policy.AllowedAgentIDs, "b")
	if got, _ := snapshot.ToolPolicy("p", "tool"); len(got.AllowedAgentIDs) != 1 {
		t.Fatalf("ToolPolicy() shared backing array: %+v", got)
	}

	agent, _ := snapshot.Agent("a")
	agent.Capabilities = append(agent.Capabilities, "vision")
	if got, _ := snapshot.Agent("a"); len(got.Capabilities) != 1 {
		t.Fatalf("Agent() shared backing array: %+v", got)
	}

	endpoint, _ := snapshot.AgentEndpoint("e")
	endpoint.Capabilities = append(endpoint.Capabilities, "x")
	endpoint.Headers["X-A2A-Tenant"] = "mutated"
	if got, _ := snapshot.AgentEndpoint("e"); len(got.Capabilities) != 1 || got.Headers["X-A2A-Tenant"] != "acme" || got.AuthScheme != "bearer" || got.SecretRef != "local://cred/e" {
		t.Fatalf("AgentEndpoint() shared backing storage: %+v", got)
	}

	relationship, _ := snapshot.FederationRelationship("rel")
	relationship.ProjectGrants = append(relationship.ProjectGrants, "mutated")
	relationship.CapabilityGrants = append(relationship.CapabilityGrants, "mutated")
	relationship.ProcessingRegions = append(relationship.ProcessingRegions, "mutated")
	if got, _ := snapshot.FederationRelationship("rel"); len(got.ProjectGrants) != 1 || len(got.CapabilityGrants) != 1 || len(got.ProcessingRegions) != 1 || got.Direction != "outbound" || got.BoundaryStatus != "declared" || got.ApprovedVersion != "1" {
		t.Fatalf("FederationRelationship() shared backing storage: %+v", got)
	}

	// The endpoint indexes its owning agent, which is deduplicated.
	if ids := snapshot.AgentsByCapability("chat"); len(ids) != 1 || ids[0] != "a" {
		t.Fatalf("AgentsByCapability(chat) = %v, want [a]", ids)
	}
	if ids := snapshot.AgentsByCapability("missing"); len(ids) != 0 {
		t.Fatalf("AgentsByCapability(missing) = %v, want none", ids)
	}
}

func TestSnapshotCollectionGettersReturnIsolatedCopies(t *testing.T) {
	snapshot := NewTenantSnapshot(TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Version: 1,
		Deployments:    []Deployment{{ID: "d", ProviderID: "pv", Status: "enabled", Capabilities: []string{"chat"}}},
		RoutePolicies:  []RoutePolicy{{ID: "r", Strategy: "priority", DeploymentIDs: []string{"d"}}},
		Tools:          []Tool{{ID: "tool", ServerID: "srv", Name: "tool", Status: "active", Schema: []byte(`{}`)}},
		Agents:         []Agent{{ID: "a", ProjectID: "p", Name: "a", Status: "active", Capabilities: []string{"chat"}}},
		LogicalModels:  []LogicalModel{{ID: "l1", Alias: "zeta", RoutePolicyID: "r"}, {ID: "l2", Alias: "alpha", RoutePolicyID: "r"}},
		BudgetPolicies: []BudgetPolicy{{ID: "b2", TenantID: "tenant"}, {ID: "b1", TenantID: "tenant"}},
		FederationRelationships: []FederationRelationship{{
			ID: "rel", ExternalAgentID: "ext", Status: "active", ProjectGrants: []string{"p"}, CapabilityGrants: []string{"chat"},
		}},
		Guardrail: GuardrailPolicy{Mode: "shadow", Rules: []GuardrailRule{{ID: "g", Kind: "pattern"}}},
	})

	if deployments := snapshot.Deployments(); len(deployments) != 1 {
		t.Fatalf("Deployments() = %v, want 1 entry", deployments)
	}
	snapshot.Deployments()[0].Capabilities = append(snapshot.Deployments()[0].Capabilities, "mutated")
	if got, _ := snapshot.Deployment("d"); len(got.Capabilities) != 1 {
		t.Fatalf("Deployments() shares entries with the snapshot: %+v", got)
	}

	snapshot.Routes()[0].DeploymentIDs = append(snapshot.Routes()[0].DeploymentIDs, "x")
	if got, _ := snapshot.RoutePolicy("r"); len(got.DeploymentIDs) != 1 {
		t.Fatalf("Routes() shares entries with the snapshot: %+v", got)
	}

	snapshot.Tools()[0].Schema = append(snapshot.Tools()[0].Schema, 'x')
	if got, _ := snapshot.Tool("tool"); len(got.Schema) != 2 {
		t.Fatalf("Tools() shares entries with the snapshot: %+v", got)
	}

	snapshot.Agents()[0].Capabilities = append(snapshot.Agents()[0].Capabilities, "mutated")
	if got, _ := snapshot.Agent("a"); len(got.Capabilities) != 1 {
		t.Fatalf("Agents() shares entries with the snapshot: %+v", got)
	}

	alias := snapshot.GuardrailPolicy()
	alias.Rules = append(alias.Rules, GuardrailRule{ID: "g2", Kind: "pattern"})
	if got := snapshot.GuardrailPolicy(); len(got.Rules) != 1 {
		t.Fatalf("GuardrailPolicy() shares rules with the snapshot: %+v", got)
	}

	models := snapshot.LogicalModels()
	if len(models) != 2 || models[0].Alias != "alpha" || models[1].Alias != "zeta" {
		t.Fatalf("LogicalModels() = %v, want deterministic alias order", models)
	}
	budgets := snapshot.BudgetPolicies()
	if len(budgets) != 2 || budgets[0].ID != "b1" || budgets[1].ID != "b2" {
		t.Fatalf("BudgetPolicies() = %v, want deterministic id order", budgets)
	}

	relationships := snapshot.FederationRelationships()
	if len(relationships) != 1 {
		t.Fatalf("FederationRelationships() = %v, want 1 entry", relationships)
	}
	relationships[0].ProjectGrants = append(relationships[0].ProjectGrants, "mutated")
	relationships[0].CapabilityGrants = append(relationships[0].CapabilityGrants, "mutated")
	if rel, _ := snapshot.FederationRelationship("rel"); len(rel.ProjectGrants) != 1 || len(rel.CapabilityGrants) != 1 {
		t.Fatalf("FederationRelationships() shares entries with the snapshot: %+v", rel)
	}
}
