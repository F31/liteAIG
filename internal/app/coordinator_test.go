package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/finops/budget"
	"github.com/F31/liteAIG/internal/platform/coordination"
)

// unsafeLedger simulates the distributed budget ledger being unavailable
// (Redis down). It returns coordination.ErrLedgerUnavailable, the sentinel the
// §14.4 fail modes key on.
type unsafeLedger struct{}

func (unsafeLedger) Reserve(context.Context, coordination.ReserveRequest) (coordination.ReserveResult, error) {
	return coordination.ReserveResult{}, coordination.ErrLedgerUnavailable
}
func (unsafeLedger) Reconcile(context.Context, string, string, float64) error {
	return coordination.ErrLedgerUnavailable
}
func (unsafeLedger) Release(context.Context, string, string) error {
	return coordination.ErrLedgerUnavailable
}
func (unsafeLedger) Sweep(context.Context, string, int) (int, error) {
	return 0, coordination.ErrLedgerUnavailable
}

func TestCoordinatorMemoryDefault(t *testing.T) {
	clock := fixedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	coord, err := openCoordinator(context.Background(), coordinatorOptions{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	defer coord.close()
	if coord.leases == nil || coord.inflight == nil || coord.ledger == nil {
		t.Fatal("memory coordinator must provide leases, inflight, and ledger")
	}
	if coord.client != nil {
		t.Fatal("memory coordinator must not open a Redis client")
	}
}

func TestCoordinatorEmptyURLBuildsCleanly(t *testing.T) {
	clock := fixedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	coord, err := openCoordinator(context.Background(), coordinatorOptions{URL: "  "}, clock)
	if err != nil {
		t.Fatalf("blank URL must fall back to memory, got %v", err)
	}
	defer coord.close()
}

func TestCoordinatorBadRedisURLFails(t *testing.T) {
	clock := fixedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	if _, err := openCoordinator(context.Background(), coordinatorOptions{URL: "not-a-redis-url"}, clock); err == nil {
		t.Fatal("malformed coordinator URL must fail at startup")
	}
}

func TestCoordinatorUnreachableRedisFailsFast(t *testing.T) {
	clock := fixedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	start := time.Now()
	if _, err := openCoordinator(context.Background(), coordinatorOptions{URL: "redis://127.0.0.1:6398/0"}, clock); err == nil {
		t.Skip("redis://127.0.0.1:6398/0 happened to be reachable; skipping")
	}
	// The eager startup Ping must fail fast (bounded by coordinatorDialTimeout).
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("unreachable coordinator took %s to fail; DialTimeout was not applied", elapsed)
	}
}

// §14.4: a hard budget fails closed when the distributed ledger is unavailable.
func TestBudgetLedgerHardFailsClosedOnUnavailable(t *testing.T) {
	enforcer := newBudgetEnforcer(budget.New(fixedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)})).
		withDistributedLedger(unsafeLedger{}, fixedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}, nil)
	policy := budget.Policy{ID: "policy-1", TenantID: "tenant-1", Mode: "hard", TokenLimit: 100,
		WindowStart: time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC), WindowEnd: time.Date(2026, 1, 1, 13, 0, 0, 0, time.UTC)}
	_, err := enforcer.Reserve(policy, "r1", 10)
	if !errors.Is(err, coordination.ErrLedgerUnavailable) {
		t.Fatalf("hard budget with unavailable ledger err = %v, want ErrLedgerUnavailable", err)
	}
}

// §14.4: a soft budget fails open when the distributed ledger is unavailable —
// the request is admitted and an alert is recorded.
func TestBudgetLedgerSoftFailsOpenOnUnavailable(t *testing.T) {
	var alerts []string
	enforcer := newBudgetEnforcer(budget.New(fixedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)})).
		withDistributedLedger(unsafeLedger{}, fixedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}, func(kind, _ string) { alerts = append(alerts, kind) })
	policy := budget.Policy{ID: "policy-soft", TenantID: "tenant-1", Mode: "soft", TokenLimit: 100,
		WindowStart: time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC), WindowEnd: time.Date(2026, 1, 1, 13, 0, 0, 0, time.UTC)}
	reserved, err := enforcer.Reserve(policy, "r1", 10)
	if err != nil {
		t.Fatalf("soft budget with unavailable ledger must fail open, got %v", err)
	}
	if reserved.ID != "r1" {
		t.Fatalf("soft fail-open reservation = %+v", reserved)
	}
	if len(alerts) != 1 || alerts[0] != "budget.fail_open" {
		t.Fatalf("soft fail-open alerts = %v", alerts)
	}
}

// A working distributed ledger still admits a hard reservation within limit.
type okLedger struct{}

func (okLedger) Reserve(_ context.Context, req coordination.ReserveRequest) (coordination.ReserveResult, error) {
	return coordination.ReserveResult{Reserved: true}, nil
}
func (okLedger) Reconcile(context.Context, string, string, float64) error { return nil }
func (okLedger) Release(context.Context, string, string) error            { return nil }
func (okLedger) Sweep(context.Context, string, int) (int, error)          { return 0, nil }

func TestBudgetLedgerHardAdmitsWhenUp(t *testing.T) {
	enforcer := newBudgetEnforcer(budget.New(fixedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)})).
		withDistributedLedger(okLedger{}, fixedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}, nil)
	policy := budget.Policy{ID: "policy-up", TenantID: "tenant-1", Mode: "hard", TokenLimit: 100,
		WindowStart: time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC), WindowEnd: time.Date(2026, 1, 1, 13, 0, 0, 0, time.UTC)}
	reserved, err := enforcer.Reserve(policy, "r1", 10)
	if err != nil {
		t.Fatalf("hard budget with available ledger must admit, got %v", err)
	}
	if reserved.ID != "r1" {
		t.Fatalf("admitted reservation = %+v", reserved)
	}
}

// A hard budget whose distributed window is exhausted (Reserved=false) rejects,
// so the cross-process limit actually holds even when the local Manager is
// still under its projection.
type exhaustedLedger struct{}

func (exhaustedLedger) Reserve(_ context.Context, req coordination.ReserveRequest) (coordination.ReserveResult, error) {
	return coordination.ReserveResult{Reserved: false}, nil
}
func (exhaustedLedger) Reconcile(context.Context, string, string, float64) error { return nil }
func (exhaustedLedger) Release(context.Context, string, string) error            { return nil }
func (exhaustedLedger) Sweep(context.Context, string, int) (int, error)          { return 0, nil }

func TestBudgetLedgerHardRejectsWhenDistributedExhausted(t *testing.T) {
	enforcer := newBudgetEnforcer(budget.New(fixedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)})).
		withDistributedLedger(exhaustedLedger{}, fixedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}, nil)
	policy := budget.Policy{ID: "policy-exh", TenantID: "tenant-1", Mode: "hard", TokenLimit: 100,
		WindowStart: time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC), WindowEnd: time.Date(2026, 1, 1, 13, 0, 0, 0, time.UTC)}
	_, err := enforcer.Reserve(policy, "r1", 10)
	if !errors.Is(err, budget.ErrExceeded) {
		t.Fatalf("hard budget with exhausted distributed ledger err = %v, want ErrExceeded", err)
	}
	reserved, _ := enforcer.Usage("policy-exh")
	if reserved != 0 {
		t.Fatalf("local reservation rolled back = %d, want 0", reserved)
	}
}
