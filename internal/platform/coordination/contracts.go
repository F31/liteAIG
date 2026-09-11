// Package coordination defines replaceable distributed primitives for Standard/Enterprise
// coordination (Redis/Valkey) with self-contained in-memory implementations for Lite.
package coordination

import (
	"context"
	"errors"
	"time"
)

var (
	ErrReservationNotFound = errors.New("budget reservation not found")
	ErrAlreadyFinalized    = errors.New("budget reservation already finalized")
	ErrCapacityExceeded    = errors.New("concurrency capacity exceeded")
	ErrLedgerUnavailable   = errors.New("budget ledger unavailable")
)

// BudgetWindow is one counter that a reservation must respect.
type BudgetWindow struct {
	Key   string        // logical window key (must be stable for a given tenant/scope/window)
	Limit float64       // maximum cumulative value for the window
	TTL   time.Duration // window counter TTL; 0 leaves the counter without an explicit TTL
}

// ReserveRequest carries the windows and estimate for an atomic reserve.
type ReserveRequest struct {
	TenantID      string
	ReservationID string
	Estimate      float64
	Windows       []BudgetWindow
}

// ReserveResult reports whether the reservation was admitted.
type ReserveResult struct {
	Reserved bool
	Active   int64 // active reservations after this operation (ledger-dependent)
	Degraded bool  // true when admitted via a fail-open policy during ledger unavailability
}

// BudgetLedger enforces multi-window budget reservation atomically.
type BudgetLedger interface {
	// Reserve atomically evaluates every window and reserves the estimate only if all fit.
	Reserve(context.Context, ReserveRequest) (ReserveResult, error)
	// Reconcile applies the actual usage for a reservation exactly once.
	Reconcile(context.Context, string, string, float64) error
	// Release returns the full estimate of a reservation exactly once.
	Release(context.Context, string, string) error
	// Sweep expires abandoned reservations and releases their estimates.
	Sweep(context.Context, string, int) (int, error)
}

// LeaseCoordinator tracks concurrency leases without full-key scans.
type LeaseCoordinator interface {
	// Acquire attempts to lease capacity under a scope key. When capacity is full
	// it returns (false, "", nil) as a busy result; errors are reserved for
	// coordination transport failures.
	Acquire(context.Context, string, int, time.Duration) (bool, string, error)
	// Renew extends a lease only if it still exists.
	Renew(context.Context, string, string, time.Duration) (bool, error)
	// Release removes a lease.
	Release(context.Context, string, string) error
	// Cleanup removes expired leases for a scope.
	Cleanup(context.Context, string) error
}

// InflightCounter tracks per-scope inflight counts for quota-aware pool selection.
type InflightCounter interface {
	Incr(context.Context, string) (int64, error)
	Decr(context.Context, string) error
	Get(context.Context, string) (int64, error)
}
