package execution

import (
	"context"
	"errors"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	kernelerrors "github.com/F31/liteAIG/internal/kernel/errors"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/resilience/retry"
	routing "github.com/F31/liteAIG/internal/routing/model"
	"sync"
	"testing"
	"time"
)

type fakeInvoker struct {
	mu     sync.Mutex
	errors []error
	calls  int
	stream func(context.Context, contracts.StreamWriter) error
}

func (f *fakeInvoker) Invoke(context.Context, contracts.InvocationRequest) (*contracts.InvocationResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(f.errors) > 0 {
		err := f.errors[0]
		f.errors = f.errors[1:]
		if err != nil {
			return nil, err
		}
	}
	return &contracts.InvocationResponse{Response: &interaction.UnifiedResponse{Model: "ok"}}, nil
}
func (f *fakeInvoker) Stream(ctx context.Context, _ contracts.InvocationRequest, w contracts.StreamWriter) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.stream != nil {
		return f.stream(ctx, w)
	}
	return nil
}
func (f *fakeInvoker) Health(context.Context, contracts.TargetRef) contracts.HealthStatus {
	return contracts.HealthStatus{Healthy: true}
}
func (f *fakeInvoker) Capabilities(context.Context, contracts.TargetRef) contracts.CapabilitySet {
	return nil
}
func (f *fakeInvoker) NormalizeError(err error) *contracts.UpstreamError {
	var upstream *contracts.UpstreamError
	if errors.As(err, &upstream) {
		return upstream
	}
	return &contracts.UpstreamError{Code: "network", Retryable: true, Message: "failed"}
}

type noSleep struct{}

func (noSleep) Sleep(context.Context, time.Duration) error { return nil }

type output struct{ count int }

func (w *output) WriteChunk(context.Context, contracts.StreamChunk) error { w.count++; return nil }
func routeFixture(t *testing.T) (*runtime.TenantRuntimeSnapshot, *routing.RoutePlan) {
	t.Helper()
	snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{TenantID: "t", TenantRef: "ref", Version: 1, Projects: []runtime.Project{{ID: "p", Status: "active"}}, Providers: []runtime.Provider{{ID: "provider", Status: "enabled"}}, Credentials: []runtime.Credential{{ID: "credential", Status: "enabled"}}, Deployments: []runtime.Deployment{{ID: "a", ProviderID: "provider", CredentialID: "credential", Status: "enabled"}, {ID: "b", ProviderID: "provider", CredentialID: "credential", Status: "enabled"}}, RoutePolicies: []runtime.RoutePolicy{{ID: "route", ProjectID: "p", Strategy: "priority", DeploymentIDs: []string{"a", "b"}, Version: 1}}, LogicalModels: []runtime.LogicalModel{{Alias: "model", RoutePolicyID: "route"}}})
	plan, err := (&routing.Planner{}).Plan(routing.Input{Snapshot: snapshot, LogicalModel: "model", ProjectID: "p", RequestID: "r"})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, plan
}
func policy() retry.Policy {
	value := retry.DefaultPolicy()
	value.BaseBackoff = time.Nanosecond
	value.MaxBackoff = time.Nanosecond
	value.AttemptTimeout = time.Second
	value.TotalTimeout = time.Second
	value.StreamIdleTimeout = 50 * time.Millisecond
	return value
}
func TestRetryFallbackAndBusinessError(t *testing.T) {
	snapshot, plan := routeFixture(t)
	retryable := &contracts.UpstreamError{Code: "rate_limited", StatusCode: 429, Retryable: true, Message: "rate limited"}
	first := &fakeInvoker{errors: []error{retryable, retryable}}
	second := &fakeInvoker{}
	executor := New(policy(), StaticResolver{"a": first, "b": second}, nil, noSleep{}, nil)
	result, err := executor.Execute(context.Background(), plan, snapshot, &interaction.UnifiedRequest{})
	if err != nil || result.DeploymentID != "b" || len(result.Attempts) != 3 {
		t.Fatalf("Execute()=%+v,%v", result, err)
	}
	business := &fakeInvoker{errors: []error{&contracts.UpstreamError{Code: "bad_request", StatusCode: 400, Retryable: false, Message: "bad"}}}
	executor = New(policy(), StaticResolver{"a": business, "b": second}, nil, noSleep{}, nil)
	result, err = executor.Execute(context.Background(), plan, snapshot, &interaction.UnifiedRequest{})
	if err == nil || len(result.Attempts) != 1 || second.calls != 1 {
		t.Fatalf("business Execute()=%+v,%v secondCalls=%d", result, err, second.calls)
	}
}
func TestStreamCommitPreventsReplay(t *testing.T) {
	snapshot, plan := routeFixture(t)
	first := &fakeInvoker{stream: func(ctx context.Context, w contracts.StreamWriter) error {
		w.WriteChunk(ctx, contracts.StreamChunk{Event: interaction.StreamEvent{Delta: "visible"}})
		return errors.New("drop")
	}}
	second := &fakeInvoker{}
	writer := &output{}
	executor := New(policy(), StaticResolver{"a": first, "b": second}, nil, noSleep{}, nil)
	attempts, err := executor.ExecuteStream(context.Background(), plan, snapshot, &interaction.UnifiedRequest{}, writer)
	if err == nil || writer.count != 1 || len(attempts) != 1 || second.calls != 0 {
		t.Fatalf("ExecuteStream() attempts=%+v count=%d second=%d err=%v", attempts, writer.count, second.calls, err)
	}
}

// Unification contract: the stream path shares the runPlan attempt loop with
// Execute, so its attempt records carry the credential for attribution.
func TestStreamAttemptsCarryCredentialAndOutcome(t *testing.T) {
	snapshot, plan := routeFixture(t)
	executor := New(policy(), StaticResolver{"a": &fakeInvoker{stream: func(ctx context.Context, w contracts.StreamWriter) error {
		w.WriteChunk(ctx, contracts.StreamChunk{Event: interaction.StreamEvent{Delta: "ok"}})
		return nil
	}}}, nil, noSleep{}, nil)
	attempts, err := executor.ExecuteStream(context.Background(), plan, snapshot, &interaction.UnifiedRequest{}, &output{})
	if err != nil || len(attempts) != 1 {
		t.Fatalf("ExecuteStream()=%+v err=%v", attempts, err)
	}
	got := attempts[0]
	if got.DeploymentID != "a" || got.CredentialID != "credential" || got.Number != 1 || got.Outcome != "success" {
		t.Fatalf("attempt = %+v", got)
	}
}
func TestStreamIdleTimeoutCanFallback(t *testing.T) {
	snapshot, plan := routeFixture(t)
	first := &fakeInvoker{stream: func(ctx context.Context, _ contracts.StreamWriter) error { <-ctx.Done(); return ctx.Err() }}
	second := &fakeInvoker{}
	executor := New(policy(), StaticResolver{"a": first, "b": second}, nil, noSleep{}, nil)
	attempts, err := executor.ExecuteStream(context.Background(), plan, snapshot, &interaction.UnifiedRequest{}, &output{})
	if err != nil || len(attempts) < 2 || second.calls != 1 {
		t.Fatalf("ExecuteStream() attempts=%+v second=%d err=%v", attempts, second.calls, err)
	}
}

type recordingCircuit struct {
	mu      sync.Mutex
	allowed int
	records []circuitRecord
}

type circuitRecord struct {
	deployment string
	credential string
	success    bool
}

func (r *recordingCircuit) Allow(string, string) bool {
	r.mu.Lock()
	r.allowed++
	r.mu.Unlock()
	return true
}
func (r *recordingCircuit) Record(deployment, credential string, success bool) {
	r.mu.Lock()
	r.records = append(r.records, circuitRecord{deployment: deployment, credential: credential, success: success})
	r.mu.Unlock()
}

// R1 regression: a failed probe (including non-retryable failures) must be
// recorded so a half-open breaker can transition instead of staying stuck.
func TestNonRetryableFailureRecordsCircuit(t *testing.T) {
	snapshot, plan := routeFixture(t)
	circuit := &recordingCircuit{}
	business := &fakeInvoker{errors: []error{&contracts.UpstreamError{Code: "bad_request", StatusCode: 400, Retryable: false, Message: "bad"}}}
	executor := New(policy(), StaticResolver{"a": business}, circuit, noSleep{}, nil)
	result, err := executor.Execute(context.Background(), plan, snapshot, &interaction.UnifiedRequest{})
	if err == nil || len(result.Attempts) != 1 {
		t.Fatalf("Execute()=%+v,%v", result, err)
	}
	circuit.mu.Lock()
	defer circuit.mu.Unlock()
	if len(circuit.records) != 1 || circuit.records[0].success || circuit.records[0].deployment != "a" || circuit.records[0].credential != "credential" {
		t.Fatalf("records = %+v; a failed probe must be recorded as a failure", circuit.records)
	}
}

func TestRetryableFailureRecordsCircuit(t *testing.T) {
	snapshot, plan := routeFixture(t)
	circuit := &recordingCircuit{}
	retryable := &fakeInvoker{errors: []error{
		&contracts.UpstreamError{Code: "rate_limited", StatusCode: 429, Retryable: true, Message: "limited"},
		&contracts.UpstreamError{Code: "rate_limited", StatusCode: 429, Retryable: true, Message: "limited"},
	}}
	executor := New(policy(), StaticResolver{"a": retryable}, circuit, noSleep{}, nil)
	_, err := executor.Execute(context.Background(), plan, snapshot, &interaction.UnifiedRequest{})
	if err == nil {
		t.Fatal("expected failure after exhausting attempts")
	}
	circuit.mu.Lock()
	defer circuit.mu.Unlock()
	if len(circuit.records) != 2 || circuit.records[0].success || circuit.records[1].success {
		t.Fatalf("records = %+v; every failed attempt must be recorded", circuit.records)
	}
}

type fakeLeaseGate struct {
	mu       sync.Mutex
	busy     map[string]bool
	acquired int
}

func (g *fakeLeaseGate) Acquire(_ context.Context, _ string, deploymentID, _ string, _ int, _ time.Duration) (bool, string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.busy[deploymentID] {
		return false, "", nil
	}
	g.acquired++
	return true, "lease-" + deploymentID, nil
}
func (g *fakeLeaseGate) Release(context.Context, string, string) error { return nil }

func leaseRouteFixture(t *testing.T) (*runtime.TenantRuntimeSnapshot, *routing.RoutePlan) {
	t.Helper()
	snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "t", TenantRef: "ref", Version: 1,
		Projects:      []runtime.Project{{ID: "p", Status: "active"}},
		Providers:     []runtime.Provider{{ID: "provider", Status: "enabled"}},
		Credentials:   []runtime.Credential{{ID: "credential", Status: "enabled"}},
		Deployments:   []runtime.Deployment{{ID: "a", ProviderID: "provider", CredentialID: "credential", Status: "enabled"}, {ID: "b", ProviderID: "provider", CredentialID: "credential", Status: "enabled"}},
		Leases:        []runtime.LeaseConfig{{DeploymentID: "a", Capacity: 1, TTLSeconds: 60}, {DeploymentID: "b", Capacity: 1, TTLSeconds: 60}},
		RoutePolicies: []runtime.RoutePolicy{{ID: "route", ProjectID: "p", Strategy: "priority", DeploymentIDs: []string{"a", "b"}, Version: 1}},
		LogicalModels: []runtime.LogicalModel{{Alias: "model", RoutePolicyID: "route"}},
	})
	plan, err := (&routing.Planner{}).Plan(routing.Input{Snapshot: snapshot, LogicalModel: "model", ProjectID: "p", RequestID: "r"})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, plan
}

// R2 regression: a deployment at capacity must not kill the request; the
// executor falls through to the next deployment in the plan.
func TestLeaseBusyFallsBackToNextDeployment(t *testing.T) {
	snapshot, plan := leaseRouteFixture(t)
	gate := &fakeLeaseGate{busy: map[string]bool{"a": true}}
	first := &fakeInvoker{}
	second := &fakeInvoker{}
	executor := New(policy(), StaticResolver{"a": first, "b": second}, nil, noSleep{}, nil).WithLeases(gate)
	result, err := executor.Execute(context.Background(), plan, snapshot, &interaction.UnifiedRequest{})
	if err != nil || result.DeploymentID != "b" {
		t.Fatalf("Execute()=%+v,%v; busy deployment must fall through", result, err)
	}
	if first.calls != 0 || second.calls != 1 {
		t.Fatalf("calls a=%d b=%d", first.calls, second.calls)
	}
}

// R2 regression: when every deployment in the plan is at capacity the request
// fails with LEASE_BUSY (retryable), not a generic exhaustion error.
func TestLeaseBusyAllDeploymentsReportsLeaseBusy(t *testing.T) {
	snapshot, plan := leaseRouteFixture(t)
	gate := &fakeLeaseGate{busy: map[string]bool{"a": true, "b": true}}
	executor := New(policy(), StaticResolver{"a": &fakeInvoker{}, "b": &fakeInvoker{}}, nil, noSleep{}, nil).WithLeases(gate)
	_, err := executor.Execute(context.Background(), plan, snapshot, &interaction.UnifiedRequest{})
	var kernelErr *kernelerrors.Error
	if !errors.As(err, &kernelErr) || kernelErr.Code != "LEASE_BUSY" || !kernelErr.Retryable {
		t.Fatalf("err = %v; want retryable LEASE_BUSY", err)
	}
}
