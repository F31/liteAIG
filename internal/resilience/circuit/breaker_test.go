package circuit

import (
	"context"
	"testing"
	"time"
)

type testClock struct{ now time.Time }

func (c *testClock) Now() time.Time { return c.now }

func TestBreakerOpenProbeCloseAndReopen(t *testing.T) {
	clock := &testClock{now: time.Unix(1, 0)}
	var transitions []Transition
	breaker := New(Config{MinSamples: 2, ErrorRate: .5, Cooldown: time.Second, CooldownFactor: 2, MaxCooldown: 10 * time.Minute, Observer: func(value Transition) { transitions = append(transitions, value) }}, clock)

	breaker.Record("d", "c", false)
	breaker.Record("d", "c", false)
	if !breaker.Open("d", "c") || breaker.Allow("d", "c") {
		t.Fatal("circuit did not open")
	}
	clock.now = clock.now.Add(time.Second)
	if !breaker.Allow("d", "c") || breaker.Allow("d", "c") {
		t.Fatal("half-open probe was not exclusive")
	}
	breaker.Record("d", "c", true)
	if breaker.Open("d", "c") || !breaker.Allow("d", "c") {
		t.Fatal("successful probe did not close")
	}
	breaker.Record("d", "c", false)
	breaker.Record("d", "c", false)
	clock.now = clock.now.Add(time.Second)
	if !breaker.Allow("d", "c") {
		t.Fatal("second probe not allowed")
	}
	breaker.Record("d", "c", false)
	if !breaker.Open("d", "c") {
		t.Fatal("failed probe did not reopen")
	}

	if transitions[0].From != StateClosed || transitions[0].To != StateOpen {
		t.Fatalf("first transition = %+v", transitions[0])
	}
	if transitions[len(transitions)-1].From != StateHalfOpen || transitions[len(transitions)-1].To != StateOpen {
		t.Fatalf("last transition = %+v", transitions[len(transitions)-1])
	}
	if !hasTransition(transitions, StateHalfOpen, StateClosed) {
		t.Fatalf("missing half_open->closed: %+v", transitions)
	}
}

func TestBreakerCooldownEscalation(t *testing.T) {
	clock := &testClock{now: time.Unix(1, 0)}
	breaker := New(Config{MinSamples: 1, ErrorRate: .5, Cooldown: time.Second, CooldownFactor: 2, MaxCooldown: 10 * time.Minute}, clock)

	// First open: cooldown 1s.
	breaker.Record("d", "c", false)
	clock.now = clock.now.Add(500 * time.Millisecond)
	if breaker.Allow("d", "c") {
		t.Fatal("allowed during initial cooldown")
	}
	clock.now = clock.now.Add(1 * time.Second)
	if !breaker.Allow("d", "c") {
		t.Fatal("probe not allowed after cooldown")
	}
	breaker.Record("d", "c", false) // failed probe -> reopen with openCount=2
	// Second open: cooldown escalates to 2s.
	clock.now = clock.now.Add(1 * time.Second)
	if breaker.Allow("d", "c") {
		t.Fatal("cooldown did not escalate to 2s")
	}
	clock.now = clock.now.Add(1 * time.Second)
	if !breaker.Allow("d", "c") {
		t.Fatal("probe not allowed after escalated cooldown")
	}
}

func TestBreakerDriftReset(t *testing.T) {
	clock := &testClock{now: time.Unix(1, 0)}
	var transitions []Transition
	breaker := New(Config{MinSamples: 1, ErrorRate: .5, Cooldown: time.Second, Observer: func(value Transition) { transitions = append(transitions, value) }}, clock)

	breaker.Record("d", "c", false)
	if !breaker.Open("d", "c") {
		t.Fatal("circuit did not open")
	}
	breaker.Reset("d", "c")
	if breaker.Open("d", "c") || !breaker.Allow("d", "c") {
		t.Fatal("reset did not close the circuit")
	}
	last := transitions[len(transitions)-1]
	if last.From != StateOpen || last.To != StateReset || last.Reason != "drift" {
		t.Fatalf("drift reset transition = %+v", last)
	}
}

func TestBreakerRestartReconstructsWithoutDoubleCountingProbe(t *testing.T) {
	clock := &testClock{now: time.Unix(1, 0)}
	store := NewMemoryStore()
	breaker := New(Config{MinSamples: 1, ErrorRate: .5, Cooldown: time.Second, CooldownFactor: 2, MaxCooldown: time.Minute, Store: store}, clock)

	breaker.Record("d", "c", false) // opens, openCount=1
	if !breaker.Open("d", "c") {
		t.Fatal("circuit did not open")
	}

	// New breaker on the same store reconstructs the open state.
	clock2 := &testClock{now: time.Unix(1, 0).Add(500 * time.Millisecond)}
	restored := New(Config{MinSamples: 1, ErrorRate: .5, Cooldown: time.Second, CooldownFactor: 2, MaxCooldown: time.Minute, Store: store}, clock2)
	if err := restored.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !restored.Open("d", "c") {
		t.Fatal("restart did not reconstruct the open circuit")
	}
	if restored.Allow("d", "c") {
		t.Fatal("restored circuit allowed a call during cooldown")
	}
	// After the persisted cooldown has elapsed, exactly one probe is admitted.
	clock2.now = clock2.now.Add(1 * time.Second)
	if !restored.Allow("d", "c") {
		t.Fatal("restored circuit did not admit a probe after cooldown")
	}
	if restored.Allow("d", "c") {
		t.Fatal("second concurrent probe admitted after restart")
	}
}

func TestBreakerResetRemovesStoreFact(t *testing.T) {
	clock := &testClock{now: time.Unix(1, 0)}
	store := NewMemoryStore()
	breaker := New(Config{MinSamples: 1, ErrorRate: .5, Cooldown: time.Second, Store: store}, clock)
	breaker.Record("d", "c", false)
	breaker.Reset("d", "c")
	facts, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 {
		t.Fatalf("reset left %d facts", len(facts))
	}
}

func TestBreakerSetConfigChangesThreshold(t *testing.T) {
	clock := &testClock{now: time.Unix(1, 0)}
	breaker := New(Config{MinSamples: 10, ErrorRate: .5, Cooldown: time.Second}, clock)
	breaker.Record("d", "c", false)
	if breaker.Open("d", "c") {
		t.Fatal("circuit opened before MinSamples was lowered")
	}
	breaker.SetConfig(Config{MinSamples: 1, ErrorRate: .5, Cooldown: time.Second})
	breaker.Record("d", "c", false)
	if !breaker.Open("d", "c") {
		t.Fatal("circuit did not open after SetConfig lowered MinSamples")
	}
}

func hasTransition(transitions []Transition, from, to string) bool {
	for _, value := range transitions {
		if value.From == from && value.To == to {
			return true
		}
	}
	return false
}
