package app

import (
	"context"
	"sync"
	"time"

	"github.com/F31/liteAIG/internal/finops/budget"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/platform/coordination"
)

// sliceRegion is the single-region label the Lite profile enforces under: the
// gateway is one node, so every budget slice lives in the "global" region.
const sliceRegion = "global"

// defaultSliceOvershootRatio bounds a global_soft slice: it may admit up to
// this fraction of the policy limit beyond the strict limit before rejecting.
const defaultSliceOvershootRatio = 0.10

// budgetSweepLimit bounds each tenant's distributed-ledger sweep batch (§14.3).
const budgetSweepLimit = 500

// budgetEnforcer layers the cross-region SliceAuthority over the in-process
// budget Manager. The Manager remains the authoritative per-policy window
// ledger; when a policy declares a consistency mode (regional/global_soft/
// global_hard) the slice additionally admits each reservation with bounded
// overshoot (global_soft) or a strict bound (regional/global_hard). The slice
// counter mirrors the Manager's projected (reserved+consumed) total so the
// two never diverge.
//
// When a distributed coordinator ledger is wired in (Standard tier), policies
// are additionally enforced against the shared ledger so limits hold across
// gateway processes; Reconcile/Release settle it and Sweep expires its
// abandoned reservations. §14.4 fail mode is applied per policy: a hard policy
// fails closed when the ledger is unavailable, a soft policy fails open and
// proceeds with local Manager enforcement while recording an alert.
type budgetEnforcer struct {
	manager     *budget.Manager
	distributed coordination.BudgetLedger
	clock       contracts.Clock
	alerts      budget.AlertFunc
	mu          sync.Mutex
	slices      map[string]*sliceEntry
	// tenantByPolicy records the tenant a reservation was reserved under so
	// Reconcile/Release can settle the distributed ledger.
	tenantByPolicy map[string]string
}

// sliceEntry binds a SliceAuthority to the window it was opened for so a
// rolled-over window starts with a clean slice (old usage must not carry over).
type sliceEntry struct {
	authority   *coordination.SliceAuthority
	windowStart time.Time
}

func newBudgetEnforcer(manager *budget.Manager) *budgetEnforcer {
	return &budgetEnforcer{manager: manager, slices: map[string]*sliceEntry{}, tenantByPolicy: map[string]string{}}
}

// withDistributedLedger wires a distributed budget ledger (Standard tier). It
// returns the enforcer for chaining. Each policy is enforced against the shared
// ledger with its §14.4 fail mode (hard = fail_closed, soft = fail_open +
// alert); Reconcile/Release settle it idempotently and Sweep expires it.
func (e *budgetEnforcer) withDistributedLedger(ledger coordination.BudgetLedger, clock contracts.Clock, alerts budget.AlertFunc) *budgetEnforcer {
	e.distributed = ledger
	e.clock = clock
	e.alerts = alerts
	return e
}

// Reserve admits a reservation through the authoritative Manager and, when the
// policy declares a consistency mode, through the region slice. A slice
// rejection rolls back the Manager reservation so the window is unchanged.
// With a distributed ledger the policy's §14.4 fail mode is applied: a hard
// policy fails closed when the ledger is unavailable, a soft policy fails open
// (admitted, alerted, local-only enforcement).
func (e *budgetEnforcer) Reserve(policy budget.Policy, id string, estimate int64) (budget.Reservation, error) {
	e.recordTenant(policy.ID, policy.TenantID)
	reserved, err := e.manager.Reserve(policy, id, estimate)
	if err != nil {
		return reserved, err
	}
	if e.distributed != nil {
		mode := budget.FailClosed
		if policy.Mode == "soft" {
			mode = budget.FailOpen
		}
		failMode := budget.NewFailModeLedger(e.distributed, mode, e.alerts)
		result, reserveErr := failMode.Reserve(context.Background(), coordination.ReserveRequest{
			TenantID:      policy.TenantID,
			ReservationID: id,
			Estimate:      float64(estimate),
			Windows:       e.distributedWindows(policy),
		})
		if reserveErr != nil {
			_ = e.manager.Release(policy.ID, id)
			return budget.Reservation{}, reserveErr
		}
		// A retreating window (Reserved=false) is an exhausted budget for either
		// mode, like the Manager's own ErrExceeded.
		if !result.Reserved {
			_ = e.manager.Release(policy.ID, id)
			return budget.Reservation{}, budget.ErrExceeded
		}
	}
	if policy.Consistency == "" {
		return reserved, nil
	}
	authority := e.authorityFor(policy)
	strict := float64(policy.TokenLimit)
	overshoot := 0.0
	if consistencyMode(policy.Consistency) == coordination.BudgetGlobalSoft {
		overshoot = strict * defaultSliceOvershootRatio
	}
	if err := authority.Admit(context.Background(), sliceRegion, float64(estimate), strict, overshoot); err != nil {
		_ = e.manager.Release(policy.ID, id)
		return budget.Reservation{}, err
	}
	return reserved, nil
}

// Reconcile settles a reservation against the Manager and realigns the slice.
// When the reservation was reserved in the distributed ledger (hard policy),
// the actual usage is settled there too so the cross-process window converges
// on the true consumed value.
func (e *budgetEnforcer) Reconcile(policyID, reservationID string, actual int64) error {
	if err := e.manager.Reconcile(policyID, reservationID, actual); err != nil {
		return err
	}
	if e.distributed != nil {
		if tenantID := e.tenantFor(policyID); tenantID != "" {
			_ = e.distributed.Reconcile(context.Background(), tenantID, reservationID, float64(actual))
		}
	}
	e.syncSlice(policyID)
	return nil
}

// Release frees a reservation against the Manager and realigns the slice. The
// distributed ledger is released idempotently as well.
func (e *budgetEnforcer) Release(policyID, reservationID string) error {
	if err := e.manager.Release(policyID, reservationID); err != nil {
		return err
	}
	if e.distributed != nil {
		if tenantID := e.tenantFor(policyID); tenantID != "" {
			_ = e.distributed.Release(context.Background(), tenantID, reservationID)
		}
	}
	e.syncSlice(policyID)
	return nil
}

func (e *budgetEnforcer) Usage(policyID string) (int64, int64) {
	return e.manager.Usage(policyID)
}

// Sweep prunes the local Manager and, when wired, expires abandoned
// reservations in the distributed ledger (reservation sweeper, §14.3).
func (e *budgetEnforcer) Sweep() {
	e.manager.Sweep()
	if e.distributed != nil {
		for _, tenantID := range e.tenants() {
			_, _ = e.distributed.Sweep(context.Background(), tenantID, budgetSweepLimit)
		}
	}
}

// distributedWindows derives the shared-ledger window enforced for a policy:
// the policy token limit over its declared window, keyed by tenant + policy +
// window start so rollover starts a fresh distributed window.
func (e *budgetEnforcer) distributedWindows(policy budget.Policy) []coordination.BudgetWindow {
	ttl := policy.WindowEnd.Sub(policy.WindowStart)
	if ttl < time.Second {
		ttl = time.Hour
	}
	start := policy.WindowStart.UTC().Format("20060102150405")
	return []coordination.BudgetWindow{
		{Key: "{tenant:" + policy.TenantID + "}:budget:" + policy.ID + ":" + start, Limit: float64(policy.TokenLimit), TTL: ttl},
	}
}

func (e *budgetEnforcer) recordTenant(policyID, tenantID string) {
	if policyID == "" || tenantID == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tenantByPolicy[policyID] = tenantID
}

func (e *budgetEnforcer) tenantFor(policyID string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.tenantByPolicy[policyID]
}

func (e *budgetEnforcer) tenants() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	seen := make(map[string]bool, len(e.tenantByPolicy))
	for _, tenantID := range e.tenantByPolicy {
		seen[tenantID] = true
	}
	result := make([]string, 0, len(seen))
	for tenantID := range seen {
		result = append(result, tenantID)
	}
	return result
}

// authorityFor returns the slice authority for a policy, creating it on first
// use from the policy's consistency mode. When the policy's window rolls over
// the authority is rebuilt so the previous window's usage does not carry into
// the fresh budget (the Manager already resets its own ledger).
func (e *budgetEnforcer) authorityFor(policy budget.Policy) *coordination.SliceAuthority {
	e.mu.Lock()
	defer e.mu.Unlock()
	entry, ok := e.slices[policy.ID]
	if !ok || !entry.windowStart.Equal(policy.WindowStart) {
		authority := coordination.ForMode(policy.ID, consistencyMode(policy.Consistency))
		e.slices[policy.ID] = &sliceEntry{authority: authority, windowStart: policy.WindowStart}
		return authority
	}
	return entry.authority
}

// syncSlice realigns a policy's slice counter with the Manager's projected
// total after a settle, so the slice always mirrors the authoritative window.
func (e *budgetEnforcer) syncSlice(policyID string) {
	e.mu.Lock()
	entry := e.slices[policyID]
	e.mu.Unlock()
	if entry == nil {
		return
	}
	reserved, consumed := e.manager.Usage(policyID)
	_, _ = entry.authority.Reconcile(context.Background(), sliceRegion, float64(reserved+consumed))
}

func consistencyMode(consistency string) coordination.BudgetConsistency {
	switch consistency {
	case "regional":
		return coordination.BudgetRegional
	case "global_soft":
		return coordination.BudgetGlobalSoft
	case "global_hard":
		return coordination.BudgetGlobalHard
	default:
		return ""
	}
}
