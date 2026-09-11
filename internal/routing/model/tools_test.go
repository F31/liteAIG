package model

import (
	"testing"

	"github.com/F31/liteAIG/internal/kernel/runtime"
)

func toolsSnapshot() *runtime.TenantRuntimeSnapshot {
	return runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Status: "active", Version: 3,
		Projects:    []runtime.Project{{ID: "project", Status: "active"}},
		Providers:   []runtime.Provider{{ID: "provider", Status: "enabled"}},
		Credentials: []runtime.Credential{{ID: "credential", ProviderID: "provider", Status: "enabled"}},
		Deployments: []runtime.Deployment{
			{ID: "tools", ProviderID: "provider", CredentialID: "credential", Status: "enabled", Capabilities: []string{"chat", "tools"}, Priority: 1},
			{ID: "text-only", ProviderID: "provider", CredentialID: "credential", Status: "enabled", Capabilities: []string{"chat"}, Priority: 1},
			{ID: "undeclared", ProviderID: "provider", CredentialID: "credential", Status: "enabled", Priority: 1},
		},
		RoutePolicies: []runtime.RoutePolicy{{ID: "route", ProjectID: "project", Strategy: "priority", DeploymentIDs: []string{"tools", "text-only", "undeclared"}, Version: 2}},
		LogicalModels: []runtime.LogicalModel{{ID: "logical", Alias: "chat", RoutePolicyID: "route"}},
	})
}

func TestToolsRequestExcludesNonToolDeployments(t *testing.T) {
	plan, err := (&Planner{}).Plan(Input{Snapshot: toolsSnapshot(), LogicalModel: "chat", ProjectID: "project", RequestID: "request", RequiresTools: true})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Selected() != "tools" {
		t.Fatalf("selected=%s", plan.Selected())
	}
	evidence := plan.Evidence()
	for _, item := range evidence {
		switch item.DeploymentID {
		case "text-only":
			if item.Eligible || !hasExclusion(item, "tools_not_supported") {
				t.Fatalf("text-only must be excluded with tools_not_supported: %+v", item)
			}
		case "undeclared":
			if !item.Eligible {
				t.Fatalf("undeclared capabilities must stay eligible: %+v", item)
			}
		}
	}
}

func TestNonToolsRequestUnaffected(t *testing.T) {
	plan, err := (&Planner{}).Plan(Input{Snapshot: toolsSnapshot(), LogicalModel: "chat", ProjectID: "project", RequestID: "request"})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range plan.Evidence() {
		if !item.Eligible {
			t.Fatalf("non-tools request must not exclude on capabilities: %+v", item)
		}
	}
}

func hasExclusion(item CandidateEvidence, code string) bool {
	for _, exclusion := range item.Exclusions {
		if exclusion.Code == code {
			return true
		}
	}
	return false
}
