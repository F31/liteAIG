package budget

import (
	"context"
	"errors"

	"github.com/F31/liteAIG/internal/platform/coordination"
)

// FailMode declares behavior when the distributed ledger is unavailable.
type FailMode string

const (
	FailClosed FailMode = "hard" // reject before resolution
	FailOpen   FailMode = "soft" // proceed and alert
)

// AlertFunc records a degraded-policy event without coupling to the Alert Engine.
type AlertFunc func(kind, message string)

// FailModeLedger wraps a BudgetLedger and applies the declared fail mode.
type FailModeLedger struct {
	inner  coordination.BudgetLedger
	mode   FailMode
	alerts AlertFunc
}

// NewFailModeLedger wraps a ledger with a fail policy and optional alert hook.
func NewFailModeLedger(inner coordination.BudgetLedger, mode FailMode, alerts AlertFunc) *FailModeLedger {
	return &FailModeLedger{inner: inner, mode: mode, alerts: alerts}
}

func (f *FailModeLedger) Reserve(ctx context.Context, req coordination.ReserveRequest) (coordination.ReserveResult, error) {
	result, err := f.inner.Reserve(ctx, req)
	if err != nil && errors.Is(err, coordination.ErrLedgerUnavailable) {
		if f.mode == FailClosed {
			f.alert("budget.fail_closed", "hard budget ledger unavailable")
			return result, err
		}
		f.alert("budget.fail_open", "soft budget ledger unavailable; proceeding without enforcement")
		return coordination.ReserveResult{Reserved: true, Degraded: true}, nil
	}
	return result, err
}

func (f *FailModeLedger) Reconcile(ctx context.Context, tenantID, reservationID string, actual float64) error {
	return f.inner.Reconcile(ctx, tenantID, reservationID, actual)
}

func (f *FailModeLedger) Release(ctx context.Context, tenantID, reservationID string) error {
	return f.inner.Release(ctx, tenantID, reservationID)
}

func (f *FailModeLedger) Sweep(ctx context.Context, tenantID string, limit int) (int, error) {
	return f.inner.Sweep(ctx, tenantID, limit)
}

func (f *FailModeLedger) alert(kind, message string) {
	if f.alerts != nil {
		f.alerts(kind, message)
	}
}
