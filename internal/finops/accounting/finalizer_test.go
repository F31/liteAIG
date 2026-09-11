package accounting

import (
	"context"
	"errors"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/tenancy"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type store struct{ calls atomic.Int32 }

func (s *store) Finalize(ctx context.Context, _ Facts) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	s.calls.Add(1)
	return true, nil
}
func (s *store) GetRequest(context.Context, tenancy.TenantScope, string) (*RequestRecord, error) {
	return nil, nil
}
func (s *store) ListRequests(context.Context, tenancy.TenantScope, int) ([]RequestRecord, error) {
	return nil, nil
}
func (s *store) GetUsage(context.Context, tenancy.TenantScope, string) (*UsageRecord, error) {
	return nil, nil
}

type budget struct{ calls atomic.Int32 }

func (b *budget) Reconcile(string, string, int64) error { b.calls.Add(1); return nil }
func (b *budget) Release(string, string) error          { b.calls.Add(1); return nil }

type budgetCalls struct {
	reconciles atomic.Int32
	releases   atomic.Int32
}

func (b *budgetCalls) Reconcile(string, string, int64) error { b.reconciles.Add(1); return nil }
func (b *budgetCalls) Release(string, string) error          { b.releases.Add(1); return nil }

type failingStore struct {
	*store
	fail error
}

func (s *failingStore) Finalize(context.Context, Facts) (bool, error) { return false, s.fail }

type sink struct {
	event contracts.DomainEvent
	err   error
}

func (s *sink) Emit(_ context.Context, event contracts.DomainEvent) error {
	s.event = event
	return s.err
}
func TestFinalizerExactlyOnceAndIgnoresTelemetryFailure(t *testing.T) {
	repository := &store{}
	budget := &budget{}
	telemetry := &sink{err: errors.New("sink down")}
	actual := int64(5)
	finalizer, err := NewFinalizer(FinalizerConfig{Timeout: time.Second}, repository, budget, telemetry, Facts{UsageEventID: "u", RequestID: "r", TenantID: "t", ProjectID: "p", LogicalModel: "m", Outcome: "success", CompletedAt: time.Unix(1, 0), BudgetPolicyID: "b", ReservationID: "x", ActualTokens: &actual})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	var wait sync.WaitGroup
	for i := 0; i < 20; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := finalizer.Finalize(cancelled)
			if err != nil || result.TelemetryError == nil {
				t.Errorf("Finalize()=%+v,%v", result, err)
			}
		}()
	}
	wait.Wait()
	if repository.calls.Load() != 1 || budget.calls.Load() != 1 {
		t.Fatalf("calls store=%d budget=%d", repository.calls.Load(), budget.calls.Load())
	}
	if telemetry.event.Attributes["logical_model"] != "m" {
		t.Fatalf("event=%+v", telemetry.event)
	}
}

// TestFinalizerReleasesReservationWhenPersistenceFails guards the budget
// reservation leak: a failed accounting write used to skip the settle step
// entirely, leaving the reserved tokens held against the window forever.
func TestFinalizerReleasesReservationWhenPersistenceFails(t *testing.T) {
	repository := &failingStore{fail: errors.New("db down")}
	budget := &budgetCalls{}
	finalizer, err := NewFinalizer(FinalizerConfig{Timeout: time.Second}, repository, budget, nil, Facts{
		UsageEventID: "u", RequestID: "r", TenantID: "t", ProjectID: "p", LogicalModel: "m", Outcome: "error",
		CompletedAt: time.Unix(1, 0), BudgetPolicyID: "b", ReservationID: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := finalizer.Finalize(context.Background()); err == nil {
		t.Fatal("Finalize should surface the persistence error")
	}
	if got := budget.reconciles.Load(); got != 0 {
		t.Fatalf("reconciles=%d, want 0", got)
	}
	if got := budget.releases.Load(); got != 1 {
		t.Fatalf("releases=%d, want 1 (reservation must not leak)", got)
	}
}
