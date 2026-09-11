package golden

import (
	"context"
	"errors"
	"github.com/F31/liteAIG/internal/finops/accounting"
	"github.com/F31/liteAIG/internal/gateway/execution"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/platform/coordination"
	"github.com/F31/liteAIG/internal/platform/storage/sqlite"
	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/internal/resilience/credentialpool"
	"github.com/F31/liteAIG/internal/resilience/retry"
	routing "github.com/F31/liteAIG/internal/routing/model"
	"github.com/F31/liteAIG/internal/tenancy"
	"github.com/F31/liteAIG/migrations"
	"testing"
	"time"
)

type scenarioInvoker struct {
	errors []error
	calls  int
	delay  time.Duration
}

func (i *scenarioInvoker) Invoke(ctx context.Context, _ contracts.InvocationRequest) (*contracts.InvocationResponse, error) {
	i.calls++
	if i.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(i.delay):
		}
	}
	if len(i.errors) > 0 {
		err := i.errors[0]
		i.errors = i.errors[1:]
		if err != nil {
			return nil, err
		}
	}
	return &contracts.InvocationResponse{Response: &interaction.UnifiedResponse{Model: "physical", Usage: interaction.UnifiedUsage{InputTokens: 3, OutputTokens: 2}}}, nil
}
func (i *scenarioInvoker) Stream(context.Context, contracts.InvocationRequest, contracts.StreamWriter) error {
	return nil
}
func (i *scenarioInvoker) Health(context.Context, contracts.TargetRef) contracts.HealthStatus {
	return contracts.HealthStatus{Healthy: true}
}
func (i *scenarioInvoker) Capabilities(context.Context, contracts.TargetRef) contracts.CapabilitySet {
	return contracts.CapabilitySet{"chat": true}
}
func (i *scenarioInvoker) NormalizeError(err error) *contracts.UpstreamError {
	var upstream *contracts.UpstreamError
	if errors.As(err, &upstream) {
		return upstream
	}
	return &contracts.UpstreamError{Code: "timeout", Retryable: true, Message: "upstream timeout"}
}

type scenarioResolver map[string]contracts.InteractionInvoker

func (r scenarioResolver) Resolve(_ *runtime.TenantRuntimeSnapshot, d runtime.Deployment) (contracts.InteractionInvoker, bool) {
	value, ok := r[d.ID]
	return value, ok
}
func (r scenarioResolver) ResolveCredential(_ *runtime.TenantRuntimeSnapshot, d runtime.Deployment, credentialID string) (contracts.InteractionInvoker, bool) {
	value, ok := r[d.ID+":"+credentialID]
	if !ok {
		return r.Resolve(nil, d)
	}
	return value, true
}

type scenarioState struct {
	snapshot *runtime.TenantRuntimeSnapshot
	disabled map[string]bool
}

func (s scenarioState) Eligible(id string) bool {
	credential, ok := s.snapshot.Credential(id)
	return ok && credential.Status == "enabled" && !s.disabled[id]
}
func (s scenarioState) Inflight(string) int             { return 0 }
func (s scenarioState) Cost(string) float64             { return 0 }
func (s scenarioState) Quota(string) (float64, float64) { return 0, 100 }

type noSleep struct{}

func (noSleep) Sleep(context.Context, time.Duration) error { return nil }

type openCircuit map[string]bool

func (c openCircuit) Open(deploymentID, credentialID string) bool { return c[deploymentID] }

func scenarioBSnapshot() *runtime.TenantRuntimeSnapshot {
	return runtime.NewTenantSnapshot(runtime.TenantSnapshotData{TenantID: "tenant", TenantRef: "ref", Version: 2, Projects: []runtime.Project{{ID: "project", Status: "active"}}, Providers: []runtime.Provider{{ID: "p1", Status: "enabled"}, {ID: "p2", Status: "enabled"}}, Credentials: []runtime.Credential{{ID: "c1", ProviderID: "p1", Status: "enabled"}, {ID: "c2", ProviderID: "p1", Status: "enabled"}, {ID: "c3", ProviderID: "p2", Status: "enabled"}}, CredentialPools: []runtime.CredentialPool{{ID: "pool", ProviderID: "p1", Strategy: "round_robin", Members: []runtime.PoolMember{{CredentialID: "c1"}, {CredentialID: "c2"}}}}, Deployments: []runtime.Deployment{{ID: "d1", ProviderID: "p1", PoolID: "pool", Status: "enabled", Priority: 0, Capabilities: []string{"chat"}}, {ID: "d2", ProviderID: "p2", CredentialID: "c3", Status: "enabled", Priority: 1, Capabilities: []string{"chat"}}}, RoutePolicies: []runtime.RoutePolicy{{ID: "route", ProjectID: "project", Strategy: "priority", DeploymentIDs: []string{"d1", "d2"}, Version: 2}}, LogicalModels: []runtime.LogicalModel{{ID: "logical", Alias: "chat", RoutePolicyID: "route"}}})
}
func scenarioPlan(t *testing.T, snapshot *runtime.TenantRuntimeSnapshot, circuit routing.CircuitView) *routing.RoutePlan {
	t.Helper()
	plan, err := (&routing.Planner{}).Plan(routing.Input{Snapshot: snapshot, LogicalModel: "chat", ProjectID: "project", RequestID: "request", RequiredCapabilities: []string{"chat"}, Circuit: circuit})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
func scenarioPolicy() retry.Policy {
	p := retry.DefaultPolicy()
	p.BaseBackoff = time.Nanosecond
	p.MaxBackoff = time.Nanosecond
	p.AttemptTimeout = 20 * time.Millisecond
	p.TotalTimeout = time.Second
	return p
}

func TestScenarioBProviderFailuresRemainExplainableAndBudgetSafe(t *testing.T) {
	snapshot := scenarioBSnapshot()
	plan := scenarioPlan(t, snapshot, nil)
	rateLimited := &contracts.UpstreamError{Code: "rate_limited", StatusCode: 429, Retryable: true, Message: "rate limited"}
	serverError := &contracts.UpstreamError{Code: "server_error", StatusCode: 500, Retryable: true, Message: "server error"}
	first := &scenarioInvoker{errors: []error{rateLimited}}
	second := &scenarioInvoker{errors: []error{serverError}}
	fallback := &scenarioInvoker{}
	resolver := scenarioResolver{"d1:c1": first, "d1:c2": second, "d2:c3": fallback}
	executor := execution.New(scenarioPolicy(), resolver, nil, noSleep{}, func() time.Duration { return 0 })
	pickerFor := func(deployment runtime.Deployment) (execution.CredentialPicker, error) {
		state := scenarioState{snapshot: snapshot}
		pool, err := credentialpool.FromSnapshot(snapshot, deployment, state)
		if err != nil {
			return nil, err
		}
		return pool.NewPicker("request")
	}
	ledger := coordination.NewMemoryBudgetLedger(nil)
	windows := []coordination.BudgetWindow{{Key: "tenant", Limit: 100}, {Key: "project", Limit: 100}}
	reserved, err := ledger.Reserve(context.Background(), coordination.ReserveRequest{TenantID: "tenant", ReservationID: "reservation", Estimate: 10, Windows: windows})
	if err != nil || !reserved.Reserved {
		t.Fatalf("reserve=%+v,%v", reserved, err)
	}
	result, err := executor.ExecuteWithPool(context.Background(), plan, snapshot, &interaction.UnifiedRequest{Model: "chat"}, pickerFor)
	if err != nil {
		t.Fatal(err)
	}
	if result.DeploymentID != "d2" || len(result.Attempts) != 3 {
		t.Fatalf("result=%+v", result)
	}
	wantCreds := []string{"c1", "c2", "c3"}
	for i, want := range wantCreds {
		if result.Attempts[i].CredentialID != want {
			t.Fatalf("attempts=%+v", result.Attempts)
		}
	}
	if err := ledger.Reconcile(context.Background(), "tenant", "reservation", 5); err != nil {
		t.Fatal(err)
	}
	after, err := ledger.Reserve(context.Background(), coordination.ReserveRequest{TenantID: "tenant", ReservationID: "after", Estimate: 95, Windows: windows})
	if err != nil || !after.Reserved {
		t.Fatal("budget estimate leaked after reconcile")
	}
	// Persist explanation facts for Request Explorer.
	db, err := sqlite.Open(context.Background(), "file:scenario-b?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	tenantID := "b9000000-0000-4000-8000-000000000001"
	projectID := "b9000000-0000-4000-8000-000000000002"
	db.Exec(`INSERT INTO tenants(id,public_ref,name) VALUES ($1,'b-ref','B')`, tenantID)
	db.Exec(`INSERT INTO projects(id,tenant_id,name) VALUES ($1,$2,'P')`, projectID, tenantID)
	attempts := make([]accounting.Attempt, len(result.Attempts))
	for i, a := range result.Attempts {
		attempts[i] = accounting.Attempt{DeploymentID: a.DeploymentID, Outcome: a.Outcome, Number: a.Number, Retryable: a.Retryable}
	}
	var evidence []accounting.RouteEvidence
	for _, item := range plan.Evidence() {
		reasons := make([]string, len(item.Exclusions))
		for i, r := range item.Exclusions {
			reasons[i] = r.Code
		}
		evidence = append(evidence, accounting.RouteEvidence{DeploymentID: item.DeploymentID, Eligible: item.Eligible, Exclusions: reasons})
	}
	repo := sqlrepo.NewAccountingRepository(db)
	facts := accounting.Facts{UsageEventID: "b9000000-0000-4000-8000-000000000003", RequestID: "b9000000-0000-4000-8000-000000000004", TenantID: tenantID, ProjectID: projectID, LogicalModel: "chat", DeploymentID: "d2", Outcome: "success", UsageSource: "api", InputTokens: 3, OutputTokens: 2, Attempts: attempts, RouteEvidence: evidence, ReceivedAt: time.Now(), CompletedAt: time.Now()}
	if _, err := repo.Finalize(context.Background(), facts); err != nil {
		t.Fatal(err)
	}
	record, err := repo.GetRequest(context.Background(), tenancy.TenantScope{TenantID: tenantID}, facts.RequestID)
	if err != nil || len(record.Attempts) != 3 || len(record.RouteEvidence) == 0 {
		t.Fatalf("record=%+v err=%v", record, err)
	}
}

func TestScenarioBCircuitOpenAndCredentialExhaustionFallBack(t *testing.T) {
	snapshot := scenarioBSnapshot()
	plan := scenarioPlan(t, snapshot, openCircuit{"d1": true})
	if plan.Selected() != "d2" {
		t.Fatalf("selected=%s evidence=%+v", plan.Selected(), plan.Evidence())
	}
	found := false
	for _, item := range plan.Evidence() {
		for _, reason := range item.Exclusions {
			if reason.Code == "circuit_open" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("route evidence omitted circuit_open")
	}
	fallback := &scenarioInvoker{}
	executor := execution.New(scenarioPolicy(), scenarioResolver{"d2:c3": fallback}, nil, noSleep{}, nil)
	pickerFor := func(deployment runtime.Deployment) (execution.CredentialPicker, error) {
		state := scenarioState{snapshot: snapshot, disabled: map[string]bool{"c1": true, "c2": true}}
		pool, err := credentialpool.FromSnapshot(snapshot, deployment, state)
		if err != nil {
			return nil, err
		}
		return pool.NewPicker("request")
	}
	result, err := executor.ExecuteWithPool(context.Background(), plan, snapshot, &interaction.UnifiedRequest{Model: "chat"}, pickerFor)
	if err != nil || result.DeploymentID != "d2" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestScenarioBSlowTTFTTimesOutAndFallsBack(t *testing.T) {
	snapshot := scenarioBSnapshot()
	plan := scenarioPlan(t, snapshot, nil)
	slow := &scenarioInvoker{delay: 100 * time.Millisecond}
	fallback := &scenarioInvoker{}
	resolver := scenarioResolver{"d1:c1": slow, "d1:c2": slow, "d2:c3": fallback}
	executor := execution.New(scenarioPolicy(), resolver, nil, noSleep{}, nil)
	pickerFor := func(deployment runtime.Deployment) (execution.CredentialPicker, error) {
		pool, err := credentialpool.FromSnapshot(snapshot, deployment, scenarioState{snapshot: snapshot})
		if err != nil {
			return nil, err
		}
		return pool.NewPicker("request")
	}
	result, err := executor.ExecuteWithPool(context.Background(), plan, snapshot, &interaction.UnifiedRequest{Model: "chat"}, pickerFor)
	if err != nil || result.DeploymentID != "d2" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(result.Attempts) < 3 {
		t.Fatalf("attempts=%+v", result.Attempts)
	}
}
