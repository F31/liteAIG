package budget

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type clock struct{ now time.Time }

func (c clock) Now() time.Time { return c.now }
func policy(mode string) Policy {
	return Policy{ID: "policy", Mode: mode, TokenLimit: 10, WindowStart: time.Unix(0, 0), WindowEnd: time.Unix(100, 0)}
}
func TestHardSoftAndIdempotentReconcile(t *testing.T) {
	manager := New(clock{time.Unix(1, 0)})
	if _, err := manager.Reserve(policy("hard"), "r1", 8); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Reserve(policy("hard"), "r2", 3); !errors.Is(err, ErrExceeded) {
		t.Fatalf("error=%v", err)
	}
	if err := manager.Reconcile("policy", "r1", 6); err != nil {
		t.Fatal(err)
	}
	if err := manager.Reconcile("policy", "r1", 9); err != nil {
		t.Fatal(err)
	}
	reserved, consumed := manager.Usage("policy")
	if reserved != 0 || consumed != 6 {
		t.Fatalf("usage=%d,%d", reserved, consumed)
	}
	soft := New(clock{time.Unix(1, 0)})
	reservation, err := soft.Reserve(policy("soft"), "r", 11)
	if err != nil || !reservation.Warning {
		t.Fatalf("soft reservation=%+v,%v", reservation, err)
	}
}
func TestSweepPrunesFinalizedReservations(t *testing.T) {
	manager := New(clock{time.Unix(1, 0)})
	if _, err := manager.Reserve(policy("hard"), "r1", 5); err != nil {
		t.Fatal(err)
	}
	if err := manager.Reconcile("policy", "r1", 5); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Reserve(policy("hard"), "r2", 2); err != nil {
		t.Fatal(err)
	}
	manager.Sweep()
	state := manager.windows["policy"]
	if _, exists := state.reservations["r1"]; exists {
		t.Fatal("finalized reservation survived sweep")
	}
	if _, exists := state.reservations["r2"]; !exists {
		t.Fatal("live reservation was pruned by sweep")
	}
	reserved, consumed := manager.Usage("policy")
	if reserved != 2 || consumed != 5 {
		t.Fatalf("usage after sweep = %d,%d, want 2,5", reserved, consumed)
	}
}
func TestConcurrentHardBudget(t *testing.T) {
	manager := New(clock{time.Unix(1, 0)})
	var wait sync.WaitGroup
	allowed := make(chan bool, 20)
	for i := 0; i < 20; i++ {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			_, err := manager.Reserve(policy("hard"), string(rune('a'+i)), 1)
			allowed <- err == nil
		}(i)
	}
	wait.Wait()
	close(allowed)
	count := 0
	for value := range allowed {
		if value {
			count++
		}
	}
	if count != 10 {
		t.Fatalf("allowed=%d", count)
	}
}

// R3 regression: when the policy window rolls over the ledger must reset, so a
// hard budget exhausted in the previous window cannot block the new one.
func TestWindowRolloverResetsLedger(t *testing.T) {
	base := time.Unix(0, 0)
	manager := New(clock{base})
	first := Policy{ID: "policy", Mode: "hard", TokenLimit: 10, WindowStart: base, WindowEnd: base.Add(time.Hour)}
	if _, err := manager.Reserve(first, "r1", 10); err != nil {
		t.Fatal(err)
	}
	if err := manager.Reconcile("policy", "r1", 10); err != nil {
		t.Fatal(err)
	}
	next := base.Add(time.Hour)
	manager.clock = clock{next}
	second := Policy{ID: "policy", Mode: "hard", TokenLimit: 10, WindowStart: next, WindowEnd: next.Add(time.Hour)}
	if _, err := manager.Reserve(second, "r2", 10); err != nil {
		t.Fatalf("new window reservation = %v; rolled-over ledger must reset", err)
	}
	reserved, consumed := manager.Usage("policy")
	if reserved != 10 || consumed != 0 {
		t.Fatalf("usage=%d,%d", reserved, consumed)
	}
}

// R3 regression: a request that reserved before a rollover and settles after
// it must still reconcile, without disturbing the fresh window.
func TestStraddledReservationReconcilesAfterRollover(t *testing.T) {
	base := time.Unix(0, 0)
	manager := New(clock{base})
	first := Policy{ID: "policy", Mode: "hard", TokenLimit: 100, WindowStart: base, WindowEnd: base.Add(time.Hour)}
	if _, err := manager.Reserve(first, "straddle", 50); err != nil {
		t.Fatal(err)
	}
	next := base.Add(time.Hour)
	manager.clock = clock{next}
	second := Policy{ID: "policy", Mode: "hard", TokenLimit: 100, WindowStart: next, WindowEnd: next.Add(time.Hour)}
	if _, err := manager.Reserve(second, "r2", 20); err != nil {
		t.Fatal(err)
	}
	if err := manager.Reconcile("policy", "straddle", 60); err != nil {
		t.Fatalf("straddled reconcile = %v", err)
	}
	reserved, consumed := manager.Usage("policy")
	if reserved != 20 || consumed != 0 {
		t.Fatalf("usage=%d,%d", reserved, consumed)
	}
}

// Retired ledgers are dropped after the grace so memory stays bounded.
func TestRetiredWindowDroppedAfterGrace(t *testing.T) {
	base := time.Unix(0, 0)
	manager := New(clock{base})
	first := Policy{ID: "policy", Mode: "hard", TokenLimit: 100, WindowStart: base, WindowEnd: base.Add(time.Hour)}
	if _, err := manager.Reserve(first, "r1", 10); err != nil {
		t.Fatal(err)
	}
	next := base.Add(time.Hour)
	manager.clock = clock{next}
	second := Policy{ID: "policy", Mode: "hard", TokenLimit: 100, WindowStart: next, WindowEnd: next.Add(time.Hour)}
	if _, err := manager.Reserve(second, "r2", 5); err != nil {
		t.Fatal(err)
	}
	if err := manager.Reconcile("policy", "r1", 10); err != nil {
		t.Fatal(err)
	}
	// Past the grace, the retired ledger is swept and late settles miss.
	manager.clock = clock{next.Add(retireGrace + time.Second)}
	manager.Sweep()
	if err := manager.Reconcile("policy", "r1", 10); err == nil {
		t.Fatal("late reconcile after grace must miss the retired ledger")
	}
}
