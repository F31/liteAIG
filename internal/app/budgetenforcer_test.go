package app

import (
	"errors"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/finops/budget"
	"github.com/F31/liteAIG/internal/platform/coordination"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func budgetTestPolicy(consistency string, limit int64) budget.Policy {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	return budget.Policy{ID: "policy-1", TenantID: "tenant-1", Mode: "soft", Consistency: consistency, TokenLimit: limit, WindowStart: now.Add(-time.Hour), WindowEnd: now.Add(time.Hour)}
}

func TestBudgetEnforcerNoConsistencyUsesManagerOnly(t *testing.T) {
	e := newBudgetEnforcer(budget.New(fixedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}))
	policy := budgetTestPolicy("", 100)
	// 60+60=120 > 100 but soft mode + no slice → both admitted (soft warns only).
	if _, err := e.Reserve(policy, "r1", 60); err != nil {
		t.Fatalf("first reserve = %v", err)
	}
	if _, err := e.Reserve(policy, "r2", 60); err != nil {
		t.Fatalf("second reserve (soft, no slice) = %v", err)
	}
}

func TestBudgetEnforcerGlobalSoftBoundedOvershoot(t *testing.T) {
	e := newBudgetEnforcer(budget.New(fixedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}))
	policy := budgetTestPolicy("global_soft", 100)
	// strict 100, overshoot 10 → admits up to 110.
	if _, err := e.Reserve(policy, "r1", 105); err != nil {
		t.Fatalf("within-overshoot reserve = %v", err)
	}
	// 105+10=115 > 110 → rejected, and the Manager reservation is rolled back.
	_, err := e.Reserve(policy, "r2", 10)
	if !errors.Is(err, coordination.ErrSliceOvershoot) {
		t.Fatalf("beyond-overshoot reserve err = %v, want ErrSliceOvershoot", err)
	}
	reserved, _ := e.Usage("policy-1")
	if reserved != 105 {
		t.Fatalf("reserved after rejected rollback = %d, want 105", reserved)
	}
}

func TestBudgetEnforcerRegionalIsStrict(t *testing.T) {
	e := newBudgetEnforcer(budget.New(fixedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}))
	policy := budgetTestPolicy("regional", 100)
	if _, err := e.Reserve(policy, "r1", 100); err != nil {
		t.Fatalf("at-limit reserve = %v", err)
	}
	if _, err := e.Reserve(policy, "r2", 1); !errors.Is(err, coordination.ErrSliceOvershoot) {
		t.Fatalf("over-limit regional reserve err = %v, want ErrSliceOvershoot", err)
	}
}

func TestBudgetEnforcerSettleRealignsSlice(t *testing.T) {
	e := newBudgetEnforcer(budget.New(fixedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}))
	policy := budgetTestPolicy("global_soft", 100)
	if _, err := e.Reserve(policy, "r1", 105); err != nil {
		t.Fatalf("reserve = %v", err)
	}
	// Actual usage 20 replaces the 105 estimate → projected drops to 20.
	if err := e.Reconcile("policy-1", "r1", 20); err != nil {
		t.Fatalf("reconcile = %v", err)
	}
	// Now well under the bound again: a fresh large reserve is admitted.
	if _, err := e.Reserve(policy, "r2", 90); err != nil {
		t.Fatalf("post-settle reserve = %v", err)
	}
}

func TestBudgetEnforcerReleaseRealignsSlice(t *testing.T) {
	e := newBudgetEnforcer(budget.New(fixedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}))
	policy := budgetTestPolicy("global_soft", 100)
	if _, err := e.Reserve(policy, "r1", 105); err != nil {
		t.Fatalf("reserve = %v", err)
	}
	if err := e.Release("policy-1", "r1"); err != nil {
		t.Fatalf("release = %v", err)
	}
	if _, err := e.Reserve(policy, "r2", 105); err != nil {
		t.Fatalf("post-release reserve = %v, want admitted after full release", err)
	}
}

type mutableClock struct{ now time.Time }

func (c *mutableClock) Now() time.Time { return c.now }

// R3 regression: after the policy window rolls over, the Manager resets its
// ledger and the enforcer must rebuild the slice so the previous window's
// usage cannot reject the fresh budget.
func TestBudgetEnforcerWindowRolloverResetsSlice(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &mutableClock{now: base}
	e := newBudgetEnforcer(budget.New(clock))
	first := budget.Policy{ID: "policy-1", TenantID: "tenant-1", Mode: "hard", Consistency: "regional", TokenLimit: 100, WindowStart: base, WindowEnd: base.Add(time.Hour)}
	// Exhaust the first window's hard budget (manager + slice).
	if _, err := e.Reserve(first, "r1", 100); err != nil {
		t.Fatalf("first window reserve = %v", err)
	}
	if err := e.Reconcile("policy-1", "r1", 100); err != nil {
		t.Fatalf("reconcile = %v", err)
	}
	// Second hour: same policy ID, new window. The budget must be fresh.
	next := base.Add(time.Hour)
	clock.now = next
	second := budget.Policy{ID: "policy-1", TenantID: "tenant-1", Mode: "hard", Consistency: "regional", TokenLimit: 100, WindowStart: next, WindowEnd: next.Add(time.Hour)}
	if _, err := e.Reserve(second, "r2", 100); err != nil {
		t.Fatalf("new window reserve = %v; rolled-over slice/ledger must reset", err)
	}
	reserved, consumed := e.Usage("policy-1")
	if reserved != 100 || consumed != 0 {
		t.Fatalf("usage=%d,%d", reserved, consumed)
	}
}
