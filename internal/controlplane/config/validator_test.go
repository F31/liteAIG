package config

import "testing"

func TestValidateAcceptsViableTenantConfig(t *testing.T) {
	document := validConfig()
	if diagnostics := Validate(document); HasErrors(diagnostics) {
		t.Fatalf("Validate() = %+v", diagnostics)
	}
}

func TestValidateRejectsScopeSecretAndRouteFailures(t *testing.T) {
	document := validConfig()
	document.Credentials[0].SecretRef = "plaintext-secret"
	document.Deployments[0].TenantID = "other-tenant"
	document.RoutePolicies[0].DeploymentIDs = []string{"missing"}
	diagnostics := Validate(document)
	for _, code := range []string{"secret_ref_invalid", "tenant_scope_mismatch", "deployment_reference_missing", "route_has_no_eligible_deployment"} {
		if !hasCode(diagnostics, code) {
			t.Fatalf("Validate() missing %q: %+v", code, diagnostics)
		}
	}
}

func TestValidateForTenantRejectsForeignDocument(t *testing.T) {
	diagnostics := ValidateForTenant(validConfig(), "other-tenant")
	if !hasCode(diagnostics, "tenant_scope_mismatch") {
		t.Fatalf("ValidateForTenant() = %+v", diagnostics)
	}
}

func TestValidateRouteStrategyVocabulary(t *testing.T) {
	for _, strategy := range []string{"priority", "weighted", "round_robin", "soft"} {
		document := validConfig()
		document.RoutePolicies[0].Strategy = strategy
		if diagnostics := Validate(document); HasErrors(diagnostics) {
			t.Fatalf("strategy %q rejected: %+v", strategy, diagnostics)
		}
	}
	invalid := validConfig()
	invalid.RoutePolicies[0].Strategy = "random"
	if !hasCode(Validate(invalid), "route_strategy_invalid") {
		t.Fatal("unknown strategy accepted")
	}
}

func TestValidateBudgetConsistencyMode(t *testing.T) {
	valid := validConfig()
	valid.BudgetPolicies = []BudgetPolicy{{ID: "b", TenantID: "tenant-1", WindowHours: 24, TokenLimit: 100, Mode: "hard", Consistency: "global_soft"}}
	if diagnostics := Validate(valid); HasErrors(diagnostics) {
		t.Fatalf("valid consistency rejected: %+v", diagnostics)
	}
	invalid := validConfig()
	invalid.BudgetPolicies = []BudgetPolicy{{ID: "b", TenantID: "tenant-1", WindowHours: 24, TokenLimit: 100, Mode: "hard", Consistency: "everywhere"}}
	if !hasCode(Validate(invalid), "budget_consistency_invalid") {
		t.Fatal("invalid consistency mode accepted")
	}
	// Empty consistency defaults to regional (accepted).
	empty := validConfig()
	empty.BudgetPolicies = []BudgetPolicy{{ID: "b", TenantID: "tenant-1", WindowHours: 24, TokenLimit: 100, Mode: "hard"}}
	if diagnostics := Validate(empty); HasErrors(diagnostics) {
		t.Fatalf("empty consistency rejected: %+v", diagnostics)
	}
}

func TestValidateRouteResidencyUsesInheritedTenantPolicy(t *testing.T) {
	document := validConfig()
	document.Tenant.AllowedDataRegions = []string{"eu"}
	document.Tenant.ResidencyEnforcement = "strict"
	document.Projects[0].AllowedDataRegions = nil
	document.Projects[0].ResidencyEnforcement = ""
	document.Deployments[0].DataRegion = "us"
	if !hasCode(Validate(document), "data_residency_violation") {
		t.Fatal("inherited tenant residency policy did not gate route")
	}
}

func TestValidateRouteResidencyUsesInheritedSystemPolicy(t *testing.T) {
	document := validConfig()
	document.Tenant.AllowedDataRegions = nil
	document.Tenant.ResidencyEnforcement = ""
	document.Projects[0].AllowedDataRegions = nil
	document.Projects[0].ResidencyEnforcement = ""
	document.Deployments[0].DataRegion = "us"
	system := SystemConfig{TenantDefaults: TenantPolicyDefaults{AllowedDataRegions: []string{"eu"}, ResidencyEnforcement: "strict"}}
	if !hasCode(ValidateWithSystemDefaults(system, document), "data_residency_violation") {
		t.Fatal("inherited system residency policy did not gate route")
	}
}

func TestValidateSpoolPolicy(t *testing.T) {
	base := validConfig()
	base.AccountingSpool = SpoolPolicy{Mode: "soft", WarnThreshold: 0.7, CriticalThreshold: 0.9, HardThreshold: 1.0}
	if diagnostics := Validate(base); HasErrors(diagnostics) {
		t.Fatalf("valid spool policy rejected: %+v", diagnostics)
	}
	badMode := validConfig()
	badMode.AccountingSpool = SpoolPolicy{Mode: "everything"}
	if !hasCode(Validate(badMode), "spool_policy_invalid") {
		t.Fatal("bad spool mode accepted")
	}
	badOrder := validConfig()
	badOrder.AccountingSpool = SpoolPolicy{Mode: "hard", WarnThreshold: 0.9, CriticalThreshold: 0.7, HardThreshold: 1.0}
	if !hasCode(Validate(badOrder), "spool_threshold_order") {
		t.Fatal("mis-ordered spool thresholds accepted")
	}
}

func TestValidateCachePolicy(t *testing.T) {
	valid := validConfig()
	valid.Cache = CachePolicy{Enabled: true, TTLSeconds: 60, NamespaceVersion: 1, MaxTemperature: 1}
	if diagnostics := Validate(valid); HasErrors(diagnostics) {
		t.Fatalf("valid cache policy rejected: %+v", diagnostics)
	}
	invalid := validConfig()
	invalid.Cache = CachePolicy{Enabled: true, TTLSeconds: -1}
	if !hasCode(Validate(invalid), "cache_policy_invalid") {
		t.Fatal("invalid cache policy accepted")
	}
}

func TestValidateGuardrailPolicy(t *testing.T) {
	valid := validConfig()
	valid.Guardrail = GuardrailPolicy{
		Judge:        GuardrailJudge{Enabled: true, Model: "gpt-4o-mini", Action: "block"},
		Groundedness: GuardrailGroundedness{Enabled: true, MinOverlap: 0.2},
	}
	if diagnostics := Validate(valid); HasErrors(diagnostics) {
		t.Fatalf("valid guardrail policy rejected: %+v", diagnostics)
	}

	missingModel := validConfig()
	missingModel.Guardrail = GuardrailPolicy{Judge: GuardrailJudge{Enabled: true, Action: "block"}}
	if !hasCode(Validate(missingModel), "guardrail_policy_invalid") {
		t.Fatal("judge without model accepted")
	}

	badAction := validConfig()
	badAction.Guardrail = GuardrailPolicy{Judge: GuardrailJudge{Enabled: true, Model: "gpt-4o-mini", Action: "quarantine"}}
	if !hasCode(Validate(badAction), "guardrail_policy_invalid") {
		t.Fatal("judge with unknown action accepted")
	}

	badOverlap := validConfig()
	badOverlap.Guardrail = GuardrailPolicy{Groundedness: GuardrailGroundedness{Enabled: true, MinOverlap: 1.5}}
	if !hasCode(Validate(badOverlap), "guardrail_policy_invalid") {
		t.Fatal("groundedness out-of-range overlap accepted")
	}
}

func TestValidateAgentEndpointAuthScheme(t *testing.T) {
	valid := validConfig()
	valid.Agents = []Agent{{ID: "agent", TenantID: "tenant-1", ProjectID: "project-1", Name: "Agent", Status: "active"}}
	base := AgentEndpoint{ID: "ep", TenantID: "tenant-1", AgentID: "agent", Version: "1", URL: "http://agent.example", Protocol: "a2a"}

	noAuth := valid
	noAuth.AgentEndpoints = []AgentEndpoint{base}
	if diagnostics := Validate(noAuth); HasErrors(diagnostics) {
		t.Fatalf("no-auth endpoint rejected: %+v", diagnostics)
	}

	bearer := valid
	bearer.AgentEndpoints = []AgentEndpoint{base}
	bearer.AgentEndpoints[0].AuthScheme = "bearer"
	bearer.AgentEndpoints[0].SecretRef = "secret://tenant/a2a"
	if diagnostics := Validate(bearer); HasErrors(diagnostics) {
		t.Fatalf("bearer endpoint rejected: %+v", diagnostics)
	}

	apiKey := valid
	apiKey.AgentEndpoints = []AgentEndpoint{base}
	apiKey.AgentEndpoints[0].AuthScheme = "x-api-key"
	apiKey.AgentEndpoints[0].SecretRef = "local://credential/x"
	if diagnostics := Validate(apiKey); HasErrors(diagnostics) {
		t.Fatalf("x-api-key endpoint rejected: %+v", diagnostics)
	}

	// A scheme without a secret ref cannot authenticate.
	secretless := valid
	secretless.AgentEndpoints = []AgentEndpoint{base}
	secretless.AgentEndpoints[0].AuthScheme = "bearer"
	if !hasCode(Validate(secretless), "agent_endpoint_auth_invalid") {
		t.Fatal("scheme without a secret ref accepted")
	}

	// A secret ref without an auth scheme is unusable.
	schemeless := valid
	schemeless.AgentEndpoints = []AgentEndpoint{base}
	schemeless.AgentEndpoints[0].SecretRef = "secret://tenant/a2a"
	if !hasCode(Validate(schemeless), "agent_endpoint_auth_invalid") {
		t.Fatal("secret ref without an auth scheme accepted")
	}

	// An unknown scheme is rejected.
	unknown := valid
	unknown.AgentEndpoints = []AgentEndpoint{base}
	unknown.AgentEndpoints[0].AuthScheme = "basic"
	unknown.AgentEndpoints[0].SecretRef = "secret://tenant/a2a"
	if !hasCode(Validate(unknown), "agent_endpoint_auth_invalid") {
		t.Fatal("unknown auth scheme accepted")
	}

	// A plaintext secret ref is not a valid reference.
	plaintext := valid
	plaintext.AgentEndpoints = []AgentEndpoint{base}
	plaintext.AgentEndpoints[0].AuthScheme = "bearer"
	plaintext.AgentEndpoints[0].SecretRef = "not-a-secret-ref"
	if !hasCode(Validate(plaintext), "secret_ref_invalid") {
		t.Fatal("plaintext secret ref accepted")
	}

	// Non-http(s) URLs are rejected.
	badURL := valid
	badURL.AgentEndpoints = []AgentEndpoint{base}
	badURL.AgentEndpoints[0].URL = "ftp://agent.example"
	if !hasCode(Validate(badURL), "agent_endpoint_url_invalid") {
		t.Fatal("non-http(s) URL accepted")
	}

	// Missing required fields are rejected.
	missing := valid
	missing.AgentEndpoints = []AgentEndpoint{{ID: "ep", TenantID: "tenant-1", URL: "http://agent.example"}}
	if !hasCode(Validate(missing), "agent_endpoint_invalid") {
		t.Fatal("endpoint missing required fields accepted")
	}
}

func TestValidateAgentApprovalMode(t *testing.T) {
	valid := validConfig()
	valid.Agents = []Agent{{ID: "agent", TenantID: "tenant-1", ProjectID: "project-1", Name: "Agent", Status: "active", ApprovalMode: "inherit_project"}}
	if diagnostics := Validate(valid); HasErrors(diagnostics) {
		t.Fatalf("valid approval mode rejected: %+v", diagnostics)
	}

	invalid := validConfig()
	invalid.Agents = []Agent{{ID: "agent", TenantID: "tenant-1", ProjectID: "project-1", Name: "Agent", Status: "active", ApprovalMode: "sometimes"}}
	if !hasCode(Validate(invalid), "agent_approval_mode_invalid") {
		t.Fatal("unknown approval mode accepted")
	}
}

func validConfig() TenantConfig {
	return TenantConfig{
		SchemaVersion: SchemaV1,
		Tenant:        TenantResource{ID: "tenant-1", PublicRef: "tenant-ref", Status: "active"},
		Projects:      []Project{{ID: "project-1", TenantID: "tenant-1", Status: "active", ResidencyEnforcement: "strict", AllowedDataRegions: []string{"us"}}},
		Providers:     []Provider{{ID: "provider-1", TenantID: "tenant-1", OwnerScope: "TENANT_PRIVATE", Type: "openai", Status: "enabled"}},
		Credentials:   []Credential{{ID: "credential-1", ProviderID: "provider-1", TenantID: "tenant-1", OwnerScope: "TENANT_PRIVATE", SecretRef: "secret://tenant/provider-1", Status: "enabled"}},
		Deployments:   []Deployment{{ID: "deployment-1", TenantID: "tenant-1", ProviderID: "provider-1", CredentialID: "credential-1", UpstreamModel: "model", DataRegion: "us", Status: "enabled"}},
		RoutePolicies: []RoutePolicy{{ID: "route-1", TenantID: "tenant-1", ProjectID: "project-1", Strategy: "priority", DeploymentIDs: []string{"deployment-1"}, Version: 1}},
		LogicalModels: []LogicalModel{{ID: "logical-1", TenantID: "tenant-1", Alias: "default-chat", RoutePolicyID: "route-1"}},
	}
}

func hasCode(diagnostics []Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func TestValidateFederationRelationshipGovernanceEnums(t *testing.T) {
	valid := validConfig()
	valid.FederationRelationships = []FederationRelationship{{
		ID: "rel", TenantID: "tenant-1", ExternalAgentID: "ext", Status: "active",
		Direction: "outbound", BoundaryStatus: "contractually_bound",
	}}
	if diagnostics := Validate(valid); HasErrors(diagnostics) {
		t.Fatalf("valid federation relationship rejected: %+v", diagnostics)
	}

	unknownDirection := validConfig()
	unknownDirection.FederationRelationships = []FederationRelationship{{
		ID: "rel", TenantID: "tenant-1", ExternalAgentID: "ext", Status: "active", Direction: "sideways",
	}}
	directionDiagnostics := Validate(unknownDirection)
	if !hasCode(directionDiagnostics, "federation_direction_invalid") {
		t.Fatalf("unknown direction not flagged: %+v", directionDiagnostics)
	}
	if HasErrors(directionDiagnostics) {
		t.Fatalf("unknown but nonempty direction must not block publish: %+v", directionDiagnostics)
	}

	unknownBoundary := validConfig()
	unknownBoundary.FederationRelationships = []FederationRelationship{{
		ID: "rel", TenantID: "tenant-1", ExternalAgentID: "ext", Status: "active", BoundaryStatus: "not-a-status",
	}}
	boundaryDiagnostics := Validate(unknownBoundary)
	if !hasCode(boundaryDiagnostics, "federation_boundary_status_invalid") {
		t.Fatalf("unknown boundary_status not flagged: %+v", boundaryDiagnostics)
	}
	if HasErrors(boundaryDiagnostics) {
		t.Fatalf("unknown boundary_status must not block publish: %+v", boundaryDiagnostics)
	}
}
