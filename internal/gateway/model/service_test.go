package model

import (
	"context"
	"github.com/F31/liteAIG/internal/gateway/execution"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/resilience/retry"
	routing "github.com/F31/liteAIG/internal/routing/model"
	"testing"
	"time"
)

type invoker struct{}

func (invoker) Invoke(context.Context, contracts.InvocationRequest) (*contracts.InvocationResponse, error) {
	return &contracts.InvocationResponse{Response: &interaction.UnifiedResponse{Model: "physical"}}, nil
}
func (invoker) Stream(context.Context, contracts.InvocationRequest, contracts.StreamWriter) error {
	return nil
}
func (invoker) Health(context.Context, contracts.TargetRef) contracts.HealthStatus {
	return contracts.HealthStatus{Healthy: true}
}
func (invoker) Capabilities(context.Context, contracts.TargetRef) contracts.CapabilitySet { return nil }
func (invoker) NormalizeError(error) *contracts.UpstreamError                             { return &contracts.UpstreamError{} }
func TestResolutionInvokesConnectorContract(t *testing.T) {
	snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{TenantID: "t", TenantRef: "ref", Projects: []runtime.Project{{ID: "p", Status: "active"}}, Providers: []runtime.Provider{{ID: "provider", Status: "enabled"}}, Credentials: []runtime.Credential{{ID: "credential", Status: "enabled"}}, Deployments: []runtime.Deployment{{ID: "deployment", ProviderID: "provider", CredentialID: "credential", Status: "enabled"}}, RoutePolicies: []runtime.RoutePolicy{{ID: "route", ProjectID: "p", Strategy: "priority", DeploymentIDs: []string{"deployment"}}}, LogicalModels: []runtime.LogicalModel{{Alias: "chat", RoutePolicyID: "route"}}})
	policy := retry.DefaultPolicy()
	policy.BaseBackoff = time.Nanosecond
	executor := execution.New(policy, execution.StaticResolver{"deployment": invoker{}}, nil, retry.TimerSleeper{}, func() time.Duration { return 0 })
	service := New(&routing.Planner{}, executor)
	plan, result, err := service.Invoke(context.Background(), Input{Snapshot: snapshot, ProjectID: "p", RequestID: "r", Request: &interaction.UnifiedRequest{Model: "chat"}})
	if err != nil || plan.Selected() != "deployment" || result.Response.Model != "physical" {
		t.Fatalf("Invoke() plan=%+v result=%+v err=%v", plan, result, err)
	}
}
