package budget

import (
	"context"
	"errors"
	"testing"

	"github.com/F31/liteAIG/internal/platform/coordination"
)

type unavailableLedger struct{}

func (unavailableLedger) Reserve(context.Context, coordination.ReserveRequest) (coordination.ReserveResult, error) {
	return coordination.ReserveResult{}, coordination.ErrLedgerUnavailable
}
func (unavailableLedger) Reconcile(context.Context, string, string, float64) error {
	return coordination.ErrLedgerUnavailable
}
func (unavailableLedger) Release(context.Context, string, string) error {
	return coordination.ErrLedgerUnavailable
}
func (unavailableLedger) Sweep(context.Context, string, int) (int, error) {
	return 0, coordination.ErrLedgerUnavailable
}

func TestFailClosedRejectsWhenLedgerUnavailable(t *testing.T) {
	var alerts []string
	wrapped := NewFailModeLedger(unavailableLedger{}, FailClosed, func(kind, _ string) { alerts = append(alerts, kind) })
	_, err := wrapped.Reserve(context.Background(), coordination.ReserveRequest{ReservationID: "r", Estimate: 1, Windows: []coordination.BudgetWindow{{Key: "w", Limit: 10}}})
	if !errors.Is(err, coordination.ErrLedgerUnavailable) {
		t.Fatalf("Reserve() error = %v", err)
	}
	if len(alerts) != 1 || alerts[0] != "budget.fail_closed" {
		t.Fatalf("alerts = %v", alerts)
	}
}

func TestFailOpenProceedsAndDegrades(t *testing.T) {
	var alerts []string
	wrapped := NewFailModeLedger(unavailableLedger{}, FailOpen, func(kind, _ string) { alerts = append(alerts, kind) })
	result, err := wrapped.Reserve(context.Background(), coordination.ReserveRequest{ReservationID: "r", Estimate: 1, Windows: []coordination.BudgetWindow{{Key: "w", Limit: 10}}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Reserved || !result.Degraded {
		t.Fatalf("reserve = %+v", result)
	}
	if len(alerts) != 1 || alerts[0] != "budget.fail_open" {
		t.Fatalf("alerts = %v", alerts)
	}
}
