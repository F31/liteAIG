package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/resilience/retry"
	routing "github.com/F31/liteAIG/internal/routing/model"
)

type staticPicker struct {
	order []string
	index int
	err   error
}

func (p *staticPicker) Next() (string, error) {
	if p.err != nil {
		return "", p.err
	}
	if p.index >= len(p.order) {
		return "", errors.New("exhausted")
	}
	id := p.order[p.index]
	p.index++
	return id, nil
}

func poolSnapshot() (*runtime.TenantRuntimeSnapshot, *routing.RoutePlan) {
	snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "t", TenantRef: "ref", Version: 1,
		Projects:  []runtime.Project{{ID: "p", Status: "active"}},
		Providers: []runtime.Provider{{ID: "provider", Status: "enabled"}},
		Credentials: []runtime.Credential{
			{ID: "cred-a", ProviderID: "provider", Status: "enabled"},
			{ID: "cred-b", ProviderID: "provider", Status: "enabled"},
		},
		Deployments: []runtime.Deployment{{ID: "d", ProviderID: "provider", CredentialID: "cred-a", PoolID: "pool", Status: "enabled"}},
		CredentialPools: []runtime.CredentialPool{{
			ID: "pool", ProviderID: "provider", TenantID: "t", Strategy: "round_robin",
			Members: []runtime.PoolMember{{CredentialID: "cred-a"}, {CredentialID: "cred-b"}},
		}},
		RoutePolicies: []runtime.RoutePolicy{{ID: "route", ProjectID: "p", Strategy: "priority", DeploymentIDs: []string{"d"}, Version: 1}},
		LogicalModels: []runtime.LogicalModel{{ID: "logical", Alias: "chat", RoutePolicyID: "route"}},
	})
	plan, err := (&routing.Planner{}).Plan(routing.Input{Snapshot: snapshot, LogicalModel: "chat", ProjectID: "p", RequestID: "r"})
	if err != nil {
		panic(err)
	}
	return snapshot, plan
}

func poolPolicy() retry.Policy {
	value := retry.DefaultPolicy()
	value.BaseBackoff = time.Nanosecond
	value.MaxBackoff = time.Nanosecond
	value.AttemptTimeout = time.Second
	value.TotalTimeout = time.Second
	return value
}

func TestExecuteWithPoolRotatesOnRetryableFailure(t *testing.T) {
	snapshot, plan := poolSnapshot()
	retryable := &contracts.UpstreamError{Code: "rate_limited", StatusCode: 429, Retryable: true}
	first := &fakeInvoker{errors: []error{retryable}}
	second := &fakeInvoker{}
	resolver := StaticResolver{"d:cred-a": first, "d:cred-b": second}
	executor := New(poolPolicy(), resolver, nil, noSleep{}, nil)
	picker := &staticPicker{order: []string{"cred-a", "cred-b"}}

	result, err := executor.ExecuteWithPool(context.Background(), plan, snapshot, &interaction.UnifiedRequest{}, func(runtime.Deployment) (CredentialPicker, error) {
		return picker, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.DeploymentID != "d" {
		t.Fatalf("deployment = %s", result.DeploymentID)
	}
	if len(result.Attempts) != 2 {
		t.Fatalf("attempts = %+v", result.Attempts)
	}
	if result.Attempts[0].CredentialID != "cred-a" || result.Attempts[1].CredentialID != "cred-b" {
		t.Fatalf("attempts = %+v", result.Attempts)
	}
	if second.calls != 1 {
		t.Fatalf("rotated credential calls = %d", second.calls)
	}
}

func TestExecuteWithPoolExhaustionFails(t *testing.T) {
	snapshot, plan := poolSnapshot()
	retryable := &contracts.UpstreamError{Code: "rate_limited", StatusCode: 429, Retryable: true}
	first := &fakeInvoker{errors: []error{retryable}}
	second := &fakeInvoker{errors: []error{retryable}}
	resolver := StaticResolver{"d:cred-a": first, "d:cred-b": second}
	executor := New(poolPolicy(), resolver, nil, noSleep{}, nil)
	picker := &staticPicker{order: []string{"cred-a", "cred-b"}}

	result, err := executor.ExecuteWithPool(context.Background(), plan, snapshot, &interaction.UnifiedRequest{}, func(runtime.Deployment) (CredentialPicker, error) {
		return picker, nil
	})
	if err == nil {
		t.Fatal("expected exhaustion error")
	}
	if len(result.Attempts) != 2 {
		t.Fatalf("attempts = %+v", result.Attempts)
	}
}
