package guardrail

import (
	"context"
	"errors"
	"testing"

	"github.com/F31/liteAIG/internal/guardrail/builtin"
	"github.com/F31/liteAIG/internal/tenancy"
)

func testPolicy() Policy {
	return Policy{ID: "policy", TenantID: "tenant", Rules: []builtin.Rule{{ID: "deny", Kind: "keyword", Pattern: "blocked", Action: "block"}}}
}

func TestPolicyTestsAndExplicitPreview(t *testing.T) {
	policy := testPolicy()
	results, err := RunTests(policy, []TestCase{{ID: "deny", Input: "blocked", ExpectedBlocked: true}, {ID: "allow", Input: "safe", ExpectedBlocked: false}})
	if err != nil || len(results) != 2 || !results[0].Passed || !results[1].Passed {
		t.Fatalf("RunTests() = %+v, %v", results, err)
	}
	preview, err := ImpactPreview(policy, []string{"safe", "blocked"})
	if err != nil || preview.Samples != 2 || preview.Matched != 1 || preview.Blocked != 1 {
		t.Fatalf("ImpactPreview() = %+v, %v", preview, err)
	}
}

func TestFastPublishRollsBackOnStoreFailure(t *testing.T) {
	store := &flakyStore{}
	registry := NewPolicyRegistryWithStore(nil, store)
	scope := tenancy.TenantScope{TenantID: "tenant"}

	store.fail = true
	if _, err := registry.FastPublish(context.Background(), scope, testPolicy(), ChangeTighten, "admin"); err == nil {
		t.Fatal("expected store failure to fail the publish")
	}
	// The in-memory policy must not have advanced on a failed persist.
	if policy, ok := registry.Active(context.Background(), scope); ok || policy.Version != 0 {
		t.Fatalf("policy advanced despite store failure: %+v, %t", policy, ok)
	}

	store.fail = false
	published, err := registry.FastPublish(context.Background(), scope, testPolicy(), ChangeTighten, "admin")
	if err != nil || published.Version != 1 {
		t.Fatalf("recovered publish = %+v, %v", published, err)
	}
	active, ok := registry.Active(context.Background(), scope)
	if !ok || active.Version != 1 {
		t.Fatalf("active after recovery = %+v, %t", active, ok)
	}
}

func TestFastPublishEpochAndScope(t *testing.T) {
	audit := &auditRecorder{}
	registry := NewPolicyRegistry(audit)
	scope := tenancy.TenantScope{TenantID: "tenant"}
	first, err := registry.FastPublish(context.Background(), scope, testPolicy(), ChangeTighten, "admin")
	if err != nil || first.Version != 1 || first.SecurityEpoch != 1 || audit.calls != 1 {
		t.Fatalf("first publish = %+v, %v audit=%d", first, err, audit.calls)
	}
	second, err := registry.FastPublish(context.Background(), scope, testPolicy(), ChangeLoosen, "admin")
	if err != nil || second.Version != 2 || second.SecurityEpoch != 2 {
		t.Fatalf("second publish = %+v, %v", second, err)
	}
	foreign := testPolicy()
	foreign.TenantID = "other"
	if _, err := registry.FastPublish(context.Background(), scope, foreign, ChangeTighten, "admin"); err != tenancy.ErrScopeMismatch {
		t.Fatalf("cross-tenant publish error = %v", err)
	}
}

type auditRecorder struct{ calls int }

func (a *auditRecorder) RecordGuardrailPublish(context.Context, tenancy.TenantScope, Policy, ChangeType, string) error {
	a.calls++
	return nil
}

type flakyStore struct{ fail bool }

func (s *flakyStore) Create(context.Context, tenancy.TenantScope, Policy, ChangeType, string) error {
	if s.fail {
		return errors.New("store unavailable")
	}
	return nil
}
func (s *flakyStore) GetActive(context.Context, tenancy.TenantScope) (*Policy, error) {
	return nil, tenancy.ErrNotFound
}
