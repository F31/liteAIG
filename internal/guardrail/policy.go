// Package guardrail owns publishable policies, tests, previews, and security events.
package guardrail

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/F31/liteAIG/internal/guardrail/builtin"
	"github.com/F31/liteAIG/internal/tenancy"
)

type ChangeType string

const (
	ChangeTighten ChangeType = "tighten"
	ChangeLoosen  ChangeType = "loosen"
)

type Policy struct {
	ID, TenantID           string
	Version, SecurityEpoch int64
	Rules                  []builtin.Rule
	ChangeType             ChangeType
	PublishedAt            time.Time
}

type TestCase struct {
	ID, Input       string
	ExpectedBlocked bool
}

type TestResult struct {
	TestCaseID      string
	Passed, Blocked bool
}

type Preview struct {
	Samples, Matched, Blocked int
}

func RunTests(policy Policy, cases []TestCase) ([]TestResult, error) {
	engine, err := builtin.New(builtin.Policy{Version: policy.Version, Rules: policy.Rules})
	if err != nil {
		return nil, err
	}
	result := make([]TestResult, 0, len(cases))
	for _, item := range cases {
		blocked := engine.Evaluate(item.Input).Blocked
		result = append(result, TestResult{TestCaseID: item.ID, Blocked: blocked, Passed: blocked == item.ExpectedBlocked})
	}
	return result, nil
}

// ImpactPreview evaluates only the explicit samples supplied by the caller.
func ImpactPreview(policy Policy, samples []string) (Preview, error) {
	engine, err := builtin.New(builtin.Policy{Version: policy.Version, Rules: policy.Rules})
	if err != nil {
		return Preview{}, err
	}
	preview := Preview{Samples: len(samples)}
	for _, sample := range samples {
		result := engine.Evaluate(sample)
		if len(result.Matches) > 0 {
			preview.Matched++
		}
		if result.Blocked {
			preview.Blocked++
		}
	}
	return preview, nil
}

type AuditRecorder interface {
	RecordGuardrailPublish(context.Context, tenancy.TenantScope, Policy, ChangeType, string) error
}

// PolicyStore persists tenant-scoped policies so they survive restarts.
type PolicyStore interface {
	Create(context.Context, tenancy.TenantScope, Policy, ChangeType, string) error
	GetActive(context.Context, tenancy.TenantScope) (*Policy, error)
}

// PolicyRegistry atomically publishes one active policy per Tenant and persists
// it when a store is provided.
type PolicyRegistry struct {
	mu     sync.RWMutex
	active map[string]Policy
	loaded map[string]bool
	audit  AuditRecorder
	store  PolicyStore
	now    func() time.Time
}

func NewPolicyRegistry(audit AuditRecorder) *PolicyRegistry {
	return &PolicyRegistry{active: map[string]Policy{}, loaded: map[string]bool{}, audit: audit, now: time.Now}
}

// NewPolicyRegistryWithStore builds a registry that persists and reloads policies.
func NewPolicyRegistryWithStore(audit AuditRecorder, store PolicyStore) *PolicyRegistry {
	return &PolicyRegistry{active: map[string]Policy{}, loaded: map[string]bool{}, audit: audit, store: store, now: time.Now}
}

func (r *PolicyRegistry) FastPublish(ctx context.Context, scope tenancy.TenantScope, policy Policy, change ChangeType, actor string) (Policy, error) {
	if err := scope.Validate(); err != nil {
		return Policy{}, err
	}
	if policy.TenantID != scope.TenantID {
		return Policy{}, tenancy.ErrScopeMismatch
	}
	if change != ChangeTighten && change != ChangeLoosen {
		return Policy{}, errors.New("invalid guardrail change type")
	}
	if _, err := builtin.New(builtin.Policy{Version: policy.Version, Rules: policy.Rules}); err != nil {
		return Policy{}, err
	}
	// Compute the next version without committing, so a store or audit failure
	// below cannot leave the in-memory policy ahead of durable state.
	r.mu.Lock()
	current, _ := r.active[scope.TenantID]
	base := current.Version
	policy.Version = base + 1
	policy.SecurityEpoch = current.SecurityEpoch + 1
	policy.PublishedAt = r.now().UTC()
	r.mu.Unlock()
	if r.store != nil {
		if err := r.store.Create(ctx, scope, policy, change, actor); err != nil {
			return Policy{}, err
		}
	}
	if r.audit != nil {
		if err := r.audit.RecordGuardrailPublish(ctx, scope, policy, change, actor); err != nil {
			return Policy{}, err
		}
	}
	// Commit last. If a concurrent publish moved the version, surface a
	// conflict instead of clobbering it (optimistic concurrency).
	r.mu.Lock()
	if got := r.active[scope.TenantID].Version; got != base {
		r.mu.Unlock()
		return Policy{}, errors.New("guardrail publish conflict: retry")
	}
	r.active[scope.TenantID] = policy
	r.loaded[scope.TenantID] = true
	r.mu.Unlock()
	return policy, nil
}

// Active returns the active policy, loading from the store on first access.
func (r *PolicyRegistry) Active(ctx context.Context, scope tenancy.TenantScope) (Policy, bool) {
	r.mu.RLock()
	policy, ok := r.active[scope.TenantID]
	loaded := r.loaded[scope.TenantID]
	r.mu.RUnlock()
	if !ok && !loaded && r.store != nil {
		stored, err := r.store.GetActive(ctx, scope)
		if err == nil && stored != nil {
			r.mu.Lock()
			r.active[scope.TenantID] = *stored
			r.loaded[scope.TenantID] = true
			r.mu.Unlock()
			return *stored, true
		}
	}
	policy.Rules = append([]builtin.Rule(nil), policy.Rules...)
	return policy, ok
}
