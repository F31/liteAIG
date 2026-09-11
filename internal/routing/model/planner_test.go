package model

import (
	"errors"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"sync"
	"testing"
)

type circuits map[string]bool

func (c circuits) Open(deploymentID, credentialID string) bool {
	return c[deploymentID+":"+credentialID]
}
func snapshot(strategy string) *runtime.TenantRuntimeSnapshot {
	return runtime.NewTenantSnapshot(runtime.TenantSnapshotData{TenantID: "tenant", TenantRef: "ref", Status: "active", Version: 3, Projects: []runtime.Project{{ID: "project", Status: "active", AllowedDataRegions: []string{"us"}, ResidencyEnforcement: "strict"}}, Providers: []runtime.Provider{{ID: "provider", Status: "enabled"}}, Credentials: []runtime.Credential{{ID: "credential", ProviderID: "provider", Status: "enabled"}}, Deployments: []runtime.Deployment{{ID: "a", ProviderID: "provider", CredentialID: "credential", Status: "enabled", DataRegion: "us", Capabilities: []string{"chat"}, ContextWindow: 100, Priority: 2}, {ID: "b", ProviderID: "provider", CredentialID: "credential", Status: "enabled", DataRegion: "us", Capabilities: []string{"chat"}, ContextWindow: 100, Priority: 1}, {ID: "bad", ProviderID: "provider", CredentialID: "credential", Status: "enabled", DataRegion: "eu", Capabilities: []string{"embedding"}, ContextWindow: 10}}, RoutePolicies: []runtime.RoutePolicy{{ID: "route", ProjectID: "project", Strategy: strategy, DeploymentIDs: []string{"a", "b", "bad"}, Weights: map[string]int{"a": 1, "b": 10}, Version: 2}}, LogicalModels: []runtime.LogicalModel{{ID: "logical", Alias: "default-chat", RoutePolicyID: "route"}}})
}
func TestHardConstraintsAndPriorityEvidence(t *testing.T) {
	plan, err := (&Planner{}).Plan(Input{Snapshot: snapshot("priority"), Key: runtime.APIKey{ModelAllowlist: []string{"default-chat"}}, LogicalModel: "default-chat", ProjectID: "project", RequestID: "request", RequiredCapabilities: []string{"chat"}, ContextTokens: 20})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Selected() != "b" {
		t.Fatalf("selected=%s", plan.Selected())
	}
	evidence := plan.Evidence()
	if evidence[2].Eligible || len(evidence[2].Exclusions) < 3 {
		t.Fatalf("evidence=%+v", evidence[2])
	}
	fallback := plan.Fallback()
	fallback[0] = "mutated"
	if plan.Fallback()[0] != "b" {
		t.Fatal("RoutePlan fallback was mutable")
	}
}
func TestWeightedIsReproducible(t *testing.T) {
	planner := &Planner{}
	input := Input{Snapshot: snapshot("weighted"), LogicalModel: "default-chat", ProjectID: "project", RequestID: "request", RequiredCapabilities: []string{"chat"}}
	first, err := planner.Plan(input)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := planner.Plan(input)
	if first.Selected() != second.Selected() {
		t.Fatalf("weighted selection changed: %s vs %s", first.Selected(), second.Selected())
	}
}
func TestRoundRobinConcurrent(t *testing.T) {
	planner := &Planner{}
	input := Input{Snapshot: snapshot("round_robin"), LogicalModel: "default-chat", ProjectID: "project", RequiredCapabilities: []string{"chat"}}
	var wait sync.WaitGroup
	results := make(chan string, 100)
	for i := 0; i < 100; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			plan, err := planner.Plan(input)
			if err != nil {
				t.Errorf("Plan() error=%v", err)
				return
			}
			results <- plan.Selected()
		}()
	}
	wait.Wait()
	close(results)
	seen := map[string]bool{}
	for value := range results {
		seen[value] = true
	}
	if !seen["a"] || !seen["b"] {
		t.Fatalf("round robin results=%v", seen)
	}
}
func TestNoEligibleDeployment(t *testing.T) {
	_, err := (&Planner{}).Plan(Input{Snapshot: snapshot("priority"), LogicalModel: "default-chat", ProjectID: "project", RequiredCapabilities: []string{"vision"}})
	if !errors.Is(err, ErrNoEligibleDeployment) {
		t.Fatalf("error=%v", err)
	}
}

func TestDynamicHealthAndCircuitConstraints(t *testing.T) {
	plan, err := (&Planner{}).Plan(Input{Snapshot: snapshot("priority"), LogicalModel: "default-chat", ProjectID: "project", RequiredCapabilities: []string{"chat"}, Health: map[string]bool{"b": false}, Circuit: circuits{"a:credential": true}})
	if !errors.Is(err, ErrNoEligibleDeployment) {
		t.Fatalf("Plan() error=%v", err)
	}
	foundHealth, foundCircuit := false, false
	for _, item := range plan.Evidence() {
		for _, reason := range item.Exclusions {
			if reason.Code == "unhealthy" {
				foundHealth = true
			}
			if reason.Code == "circuit_open" {
				foundCircuit = true
			}
		}
	}
	if !foundHealth || !foundCircuit {
		t.Fatalf("evidence=%+v", plan.Evidence())
	}
}
