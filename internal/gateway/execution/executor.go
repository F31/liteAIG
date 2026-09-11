// Package execution invokes a fixed route plan with bounded resilience.
package execution

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	kernelerrors "github.com/F31/liteAIG/internal/kernel/errors"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/resilience/retry"
	routing "github.com/F31/liteAIG/internal/routing/model"
)

type Resolver interface {
	Resolve(snapshot *runtime.TenantRuntimeSnapshot, deployment runtime.Deployment) (contracts.InteractionInvoker, bool)
}
type Circuit interface {
	Allow(string, string) bool
	Record(string, string, bool)
}

type LeaseGate interface {
	Acquire(ctx context.Context, scope, deploymentID, credentialID string, capacity int, ttl time.Duration) (bool, string, error)
	Release(ctx context.Context, scope, leaseID string) error
}

// InflightCounter tracks shared (cross-process) in-flight attempt counts for
// quota-aware credential selection. It is optional: executors built without
// one (via WithInflight) skip tracking entirely.
type InflightCounter interface {
	Incr(ctx context.Context, scope string) (int64, error)
	Decr(ctx context.Context, scope string) error
	Get(ctx context.Context, scope string) (int64, error)
}

type Attempt struct {
	DeploymentID string
	CredentialID string
	Number       int
	Outcome      string
	Retryable    bool
}
type Result struct {
	Response     *interaction.UnifiedResponse
	Attempts     []Attempt
	DeploymentID string
}
type Executor struct {
	policy   retry.Policy
	resolver Resolver
	circuit  Circuit
	leases   LeaseGate
	sleeper  retry.Sleeper
	jitter   func() time.Duration
	inflight InflightCounter
}

// CredentialPicker yields the next credential for an attempt.
type CredentialPicker interface {
	Next() (string, error)
}

// CredentialResolver additionally resolves an invoker for a specific credential.
type CredentialResolver interface {
	Resolver
	ResolveCredential(snapshot *runtime.TenantRuntimeSnapshot, deployment runtime.Deployment, credentialID string) (contracts.InteractionInvoker, bool)
}

func New(policy retry.Policy, resolver Resolver, circuit Circuit, sleeper retry.Sleeper, jitter func() time.Duration) *Executor {
	if jitter == nil {
		jitter = func() time.Duration {
			if policy.BaseBackoff <= 0 {
				return 0
			}
			value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(policy.BaseBackoff)))
			if err != nil {
				return 0
			}
			return time.Duration(value.Int64())
		}
	}
	return &Executor{policy: policy, resolver: resolver, circuit: circuit, sleeper: sleeper, jitter: jitter}
}

func (e *Executor) WithLeases(leases LeaseGate) *Executor {
	e.leases = leases
	return e
}

// WithInflight attaches a shared inflight counter used to feed quota-aware
// credential selection (least_inflight / quota_aware) across processes.
func (e *Executor) WithInflight(inflight InflightCounter) *Executor {
	e.inflight = inflight
	return e
}

// attemptCall performs one bounded call against a deployment. For streams it
// reports committed=true as soon as the first byte left, which forbids replay.
type attemptCall func(totalCtx context.Context, invoker contracts.InteractionInvoker, deployment runtime.Deployment, request *interaction.UnifiedRequest) (response *interaction.UnifiedResponse, committed bool, err error)

// runPlan walks the route plan with the shared resilience contract: circuit
// gate, capacity lease, per-attempt timeout, retryable backoff and final
// LEASE_BUSY / exhausted classification. Execute and ExecuteStream differ
// only in the call step, so both delegate here.
func (e *Executor) runPlan(ctx context.Context, plan *routing.RoutePlan, snapshot *runtime.TenantRuntimeSnapshot, request *interaction.UnifiedRequest, call attemptCall, stopOnCommitted bool) ([]Attempt, *interaction.UnifiedResponse, string, error) {
	totalCtx, cancel := context.WithTimeout(ctx, e.policy.TotalTimeout)
	defer cancel()
	var attempts []Attempt
	var last error
	leaseBlocked := false
	calls := 0
	for _, deploymentID := range plan.Fallback() {
		deployment, ok := snapshot.Deployment(deploymentID)
		if !ok {
			continue
		}
		invoker, ok := e.resolver.Resolve(snapshot, deployment)
		if !ok {
			continue
		}
		for attempt := 1; attempt <= e.policy.MaxAttemptsPerDeployment && calls < e.policy.MaxTotalCalls; attempt++ {
			if e.circuit != nil && !e.circuit.Allow(deployment.ID, deployment.CredentialID) {
				break
			}
			leaseScope, leaseID, leased, err := e.acquireLease(totalCtx, snapshot, deployment, deployment.CredentialID)
			if err != nil {
				return attempts, nil, "", err
			}
			if !leased {
				leaseBlocked = true
				break
			}
			calls++
			decrement := e.trackInflight(snapshot, deployment, deployment.CredentialID)
			response, committed, err := call(totalCtx, invoker, deployment, request)
			decrement()
			_ = e.releaseLease(context.Background(), leaseScope, leaseID)
			if err == nil {
				if e.circuit != nil {
					e.circuit.Record(deployment.ID, deployment.CredentialID, true)
				}
				attempts = append(attempts, Attempt{DeploymentID: deployment.ID, CredentialID: deployment.CredentialID, Number: attempt, Outcome: "success"})
				return attempts, response, deployment.ID, nil
			}
			normalized := invoker.NormalizeError(err)
			last = err
			attempts = append(attempts, Attempt{DeploymentID: deployment.ID, CredentialID: deployment.CredentialID, Number: attempt, Outcome: normalized.Code, Retryable: normalized.Retryable})
			// Record the failure before any early return: a half-open probe
			// that fails with a non-retryable error must still be observed, or
			// the breaker stays half-open forever and the deployment is
			// excluded from routing permanently.
			if e.circuit != nil {
				e.circuit.Record(deployment.ID, deployment.CredentialID, false)
			}
			if (stopOnCommitted && committed) || !normalized.Retryable {
				return attempts, nil, "", err
			}
			if attempt < e.policy.MaxAttemptsPerDeployment && calls < e.policy.MaxTotalCalls {
				if err := e.sleeper.Sleep(totalCtx, e.policy.Backoff(attempt, e.jitter())); err != nil {
					return attempts, nil, "", err
				}
			}
		}
	}
	if last == nil {
		if leaseBlocked {
			last = &kernelerrors.Error{Code: "LEASE_BUSY", Message: "all deployments at capacity", Retryable: true}
		} else {
			last = errors.New("route plan exhausted")
		}
	}
	return attempts, nil, "", last
}

// boundedCall invokes the deployment with a per-attempt timeout.
func (e *Executor) boundedCall(totalCtx context.Context, invoker contracts.InteractionInvoker, deployment runtime.Deployment, request *interaction.UnifiedRequest) (*interaction.UnifiedResponse, bool, error) {
	callCtx, stop := context.WithTimeout(totalCtx, e.policy.AttemptTimeout)
	defer stop()
	response, err := invoker.Invoke(callCtx, contracts.InvocationRequest{Target: contracts.TargetRef{Kind: "model", ID: deployment.ID}, CredentialID: deployment.CredentialID, Request: request})
	if err != nil {
		return nil, false, err
	}
	return response.Response, false, nil
}

// streamCall runs the deployment through streamAttempt (idle timeout +
// commit tracking) and never returns a response body.
func (e *Executor) streamCall(writer contracts.StreamWriter) attemptCall {
	return func(totalCtx context.Context, invoker contracts.InteractionInvoker, deployment runtime.Deployment, request *interaction.UnifiedRequest) (*interaction.UnifiedResponse, bool, error) {
		committed, err := e.streamAttempt(totalCtx, invoker, deployment, request, writer)
		return nil, committed, err
	}
}

func (e *Executor) Execute(ctx context.Context, plan *routing.RoutePlan, snapshot *runtime.TenantRuntimeSnapshot, request *interaction.UnifiedRequest) (*Result, error) {
	attempts, response, deploymentID, last := e.runPlan(ctx, plan, snapshot, request, e.boundedCall, false)
	return &Result{Response: response, Attempts: attempts, DeploymentID: deploymentID}, last
}

// ExecuteWithPool selects a credential per attempt from the pool rotation order,
// rotating on retryable failures and recording per-credential attribution.
func (e *Executor) ExecuteWithPool(
	ctx context.Context,
	plan *routing.RoutePlan,
	snapshot *runtime.TenantRuntimeSnapshot,
	request *interaction.UnifiedRequest,
	pickerFor func(runtime.Deployment) (CredentialPicker, error),
) (*Result, error) {
	credentialResolver, ok := e.resolver.(CredentialResolver)
	if !ok {
		return e.Execute(ctx, plan, snapshot, request)
	}
	totalCtx, cancel := context.WithTimeout(ctx, e.policy.TotalTimeout)
	defer cancel()
	result := &Result{}
	calls := 0
	var last error
	leaseBlocked := false
	for _, deploymentID := range plan.Fallback() {
		deployment, ok := snapshot.Deployment(deploymentID)
		if !ok {
			continue
		}
		picker, err := pickerFor(deployment)
		if err != nil || picker == nil {
			continue
		}
		for attempt := 1; attempt <= e.policy.MaxAttemptsPerDeployment && calls < e.policy.MaxTotalCalls; attempt++ {
			credentialID, err := picker.Next()
			if err != nil {
				break // pool exhausted for this deployment
			}
			if e.circuit != nil && !e.circuit.Allow(deployment.ID, credentialID) {
				continue
			}
			leaseScope, leaseID, leased, err := e.acquireLease(totalCtx, snapshot, deployment, credentialID)
			if err != nil {
				return result, err
			}
			if !leased {
				leaseBlocked = true
				continue
			}
			invoker, ok := credentialResolver.ResolveCredential(snapshot, deployment, credentialID)
			if !ok {
				_ = e.releaseLease(context.Background(), leaseScope, leaseID)
				continue
			}
			calls++
			callCtx, stop := context.WithTimeout(totalCtx, e.policy.AttemptTimeout)
			decrement := e.trackInflight(snapshot, deployment, credentialID)
			response, err := invoker.Invoke(callCtx, contracts.InvocationRequest{
				Target: contracts.TargetRef{Kind: "model", ID: deployment.ID}, CredentialID: credentialID, Request: request,
			})
			stop()
			decrement()
			_ = e.releaseLease(context.Background(), leaseScope, leaseID)
			if err == nil {
				if e.circuit != nil {
					e.circuit.Record(deployment.ID, credentialID, true)
				}
				result.Response = response.Response
				result.DeploymentID = deployment.ID
				result.Attempts = append(result.Attempts, Attempt{DeploymentID: deployment.ID, CredentialID: credentialID, Number: attempt, Outcome: "success"})
				return result, nil
			}
			normalized := invoker.NormalizeError(err)
			last = err
			result.Attempts = append(result.Attempts, Attempt{DeploymentID: deployment.ID, CredentialID: credentialID, Number: attempt, Outcome: normalized.Code, Retryable: normalized.Retryable})
			// Record before any early return so a failed half-open probe is
			// always observed by the breaker (see the execute path above).
			if e.circuit != nil {
				e.circuit.Record(deployment.ID, credentialID, false)
			}
			if !normalized.Retryable {
				return result, err
			}
			if calls < e.policy.MaxTotalCalls {
				if err := e.sleeper.Sleep(totalCtx, e.policy.Backoff(attempt, e.jitter())); err != nil {
					return result, err
				}
			}
		}
	}
	if last == nil {
		if leaseBlocked {
			last = &kernelerrors.Error{Code: "LEASE_BUSY", Message: "all deployments at capacity", Retryable: true}
		} else {
			last = errors.New("route plan exhausted")
		}
	}
	return result, last
}
func (e *Executor) ExecuteStream(ctx context.Context, plan *routing.RoutePlan, snapshot *runtime.TenantRuntimeSnapshot, request *interaction.UnifiedRequest, writer contracts.StreamWriter) ([]Attempt, error) {
	attempts, _, _, last := e.runPlan(ctx, plan, snapshot, request, e.streamCall(writer), true)
	return attempts, last
}

func (e *Executor) acquireLease(ctx context.Context, snapshot *runtime.TenantRuntimeSnapshot, deployment runtime.Deployment, credentialID string) (string, string, bool, error) {
	if e.leases == nil || snapshot == nil {
		return "", "", true, nil
	}
	lease, ok := snapshot.Lease(deployment.ID)
	if !ok || lease.Capacity <= 0 || lease.TTLSeconds <= 0 {
		return "", "", true, nil
	}
	scope := "{tenant:" + snapshot.TenantID + "}:deployment:" + deployment.ID
	ok, leaseID, err := e.leases.Acquire(ctx, scope, deployment.ID, credentialID, lease.Capacity, time.Duration(lease.TTLSeconds)*time.Second)
	if err != nil {
		return "", "", false, err
	}
	// Capacity busy (or circuit-open via the gate) is a normal routing
	// condition, not an error: the caller must fall through to the next
	// deployment/credential in the plan. Surfacing it as an error here made
	// the fallback paths dead code and killed the whole request.
	if !ok {
		return "", "", false, nil
	}
	return scope, leaseID, true, nil
}

func (e *Executor) releaseLease(ctx context.Context, scope, leaseID string) error {
	if e.leases == nil || scope == "" || leaseID == "" {
		return nil
	}
	return e.leases.Release(ctx, scope, leaseID)
}

// inflightScope names the shared per-deployment+credential counter for
// quota-aware selection. It mirrors the pipeline's observer scope so the count
// a pool strategy reads matches what the executor updates.
func inflightScope(tenantID, deploymentID, credentialID string) string {
	return "{tenant:" + tenantID + "}:deployment:" + deploymentID + ":credential:" + credentialID
}

// trackInflight marks one in-flight attempt against the shared counter and
// returns the deferred decrement. Without a counter it is a no-op.
func (e *Executor) trackInflight(snapshot *runtime.TenantRuntimeSnapshot, deployment runtime.Deployment, credentialID string) func() {
	if e.inflight == nil || snapshot == nil {
		return func() {}
	}
	scope := inflightScope(snapshot.TenantID, deployment.ID, credentialID)
	_, _ = e.inflight.Incr(context.Background(), scope)
	return func() {
		_ = e.inflight.Decr(context.Background(), scope)
	}
}

func (e *Executor) streamAttempt(ctx context.Context, invoker contracts.InteractionInvoker, deployment runtime.Deployment, request *interaction.UnifiedRequest, writer contracts.StreamWriter) (bool, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, e.policy.AttemptTimeout)
	defer cancel()
	tracking := &trackingWriter{target: writer, activity: make(chan struct{}, 1)}
	result := make(chan error, 1)
	go func() {
		result <- invoker.Stream(attemptCtx, contracts.InvocationRequest{Target: contracts.TargetRef{Kind: "model", ID: deployment.ID}, Request: request}, tracking)
	}()
	timer := time.NewTimer(e.policy.StreamIdleTimeout)
	defer timer.Stop()
	for {
		select {
		case err := <-result:
			return tracking.committed.Load(), err
		case <-tracking.activity:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(e.policy.StreamIdleTimeout)
		case <-timer.C:
			cancel()
			err := <-result
			if err == nil {
				err = context.DeadlineExceeded
			}
			return tracking.committed.Load(), err
		case <-ctx.Done():
			cancel()
			return tracking.committed.Load(), ctx.Err()
		}
	}
}

type trackingWriter struct {
	target    contracts.StreamWriter
	activity  chan struct{}
	committed atomic.Bool
}

func (w *trackingWriter) WriteChunk(ctx context.Context, chunk contracts.StreamChunk) error {
	w.committed.Store(true)
	if err := w.target.WriteChunk(ctx, chunk); err != nil {
		return err
	}
	select {
	case w.activity <- struct{}{}:
	default:
	}
	return nil
}

type StaticResolver map[string]contracts.InteractionInvoker

func (r StaticResolver) Resolve(_ *runtime.TenantRuntimeSnapshot, deployment runtime.Deployment) (contracts.InteractionInvoker, bool) {
	value, ok := r[deployment.ID]
	return value, ok
}

func (r StaticResolver) ResolveCredential(_ *runtime.TenantRuntimeSnapshot, deployment runtime.Deployment, credentialID string) (contracts.InteractionInvoker, bool) {
	value, ok := r[deployment.ID+":"+credentialID]
	if !ok {
		return r.Resolve(nil, deployment)
	}
	return value, true
}
