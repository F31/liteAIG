// Package budget implements Phase 0 single-window token reservations.
package budget

import (
	"errors"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"sync"
	"time"
)

var ErrExceeded = errors.New("token budget exceeded")

type Policy struct {
	ID, TenantID, ProjectID, KeyID, Mode string
	Consistency                          string // regional|global_soft|global_hard ("" = no cross-region slice)
	TokenLimit                           int64
	WindowStart, WindowEnd               time.Time
}
type Reservation struct {
	ID, PolicyID    string
	EstimatedTokens int64
	Warning         bool
}
type reservationState struct {
	estimate int64
	final    bool
}
type window struct {
	// start is the policy window this ledger was opened for, so a rollover
	// can be detected and the counters reset.
	start              time.Time
	reserved, consumed int64
	reservations       map[string]*reservationState
}

// retiredWindow holds a rolled-over ledger long enough for in-flight requests
// that reserved in the old window to still reconcile against it.
type retiredWindow struct {
	state     *window
	retiredAt time.Time
}

// sweepInterval is how often the lazy sweep inside Reserve runs. Pruning is
// amortized so the hot path pays at most one O(reservations) scan per interval.
const sweepInterval = 60 * time.Second

// retireGrace keeps rolled-over ledgers available for late reconciles. The
// accounting finalizer is bounded to seconds, so a minute covers it with
// margin while still bounding memory.
const retireGrace = time.Minute

type Manager struct {
	clock     contracts.Clock
	mu        sync.Mutex
	windows   map[string]*window
	retired   map[string]*retiredWindow
	lastSweep time.Time
}

func New(clock contracts.Clock) *Manager {
	return &Manager{clock: clock, windows: make(map[string]*window), retired: make(map[string]*retiredWindow), lastSweep: clock.Now()}
}

// sweepLocked prunes finalized reservations so long-running gateways do not
// accumulate one map entry per request, and drops retired ledgers past their
// grace. Callers must hold m.mu.
func (m *Manager) sweepLocked(now time.Time) {
	if now.Sub(m.lastSweep) < sweepInterval {
		return
	}
	m.lastSweep = now
	for _, state := range m.windows {
		for id, reservation := range state.reservations {
			if reservation.final {
				delete(state.reservations, id)
			}
		}
	}
	for id, retired := range m.retired {
		if now.Sub(retired.retiredAt) > retireGrace {
			delete(m.retired, id)
		}
	}
}
func (m *Manager) Reserve(policy Policy, id string, estimate int64) (Reservation, error) {
	if id == "" || estimate < 0 || policy.TokenLimit < 0 || (policy.Mode != "hard" && policy.Mode != "soft") || !m.clock.Now().Before(policy.WindowEnd) || m.clock.Now().Before(policy.WindowStart) {
		return Reservation{}, errors.New("invalid budget reservation")
	}
	now := m.clock.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked(now)
	state := m.windows[policy.ID]
	if state == nil || !state.start.Equal(policy.WindowStart) {
		// The window rolled over (or this is the first reservation): the old
		// ledger belongs to a previous window and must not carry its counters
		// forward, or a "daily" hard budget would permanently reject once
		// first exhausted. Retire it so in-flight reconciles still land.
		if state != nil {
			m.retired[policy.ID] = &retiredWindow{state: state, retiredAt: now}
		}
		state = &window{start: policy.WindowStart, reservations: make(map[string]*reservationState)}
		m.windows[policy.ID] = state
	}
	if existing := state.reservations[id]; existing != nil {
		return Reservation{ID: id, PolicyID: policy.ID, EstimatedTokens: existing.estimate, Warning: policy.Mode == "soft" && state.reserved+state.consumed > policy.TokenLimit}, nil
	}
	projected := state.reserved + state.consumed + estimate
	if policy.Mode == "hard" && projected > policy.TokenLimit {
		return Reservation{}, ErrExceeded
	}
	state.reserved += estimate
	state.reservations[id] = &reservationState{estimate: estimate}
	return Reservation{ID: id, PolicyID: policy.ID, EstimatedTokens: estimate, Warning: policy.Mode == "soft" && projected > policy.TokenLimit}, nil
}

// ledgerFor returns the ledger holding the reservation: the current window,
// or the retired one if the request straddled a window rollover.
func (m *Manager) ledgerFor(policyID, reservationID string) *window {
	if state, ok := m.windows[policyID]; ok {
		if _, ok := state.reservations[reservationID]; ok {
			return state
		}
	}
	if retired, ok := m.retired[policyID]; ok {
		if _, ok := retired.state.reservations[reservationID]; ok {
			return retired.state
		}
	}
	return nil
}
func (m *Manager) Reconcile(policyID, reservationID string, actual int64) error {
	if actual < 0 {
		return errors.New("actual tokens cannot be negative")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.ledgerFor(policyID, reservationID)
	if state == nil {
		return errors.New("reservation not found")
	}
	reservation := state.reservations[reservationID]
	if reservation.final {
		return nil
	}
	state.reserved -= reservation.estimate
	state.consumed += actual
	reservation.final = true
	return nil
}
func (m *Manager) Release(policyID, reservationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.ledgerFor(policyID, reservationID)
	if state == nil {
		return nil
	}
	reservation := state.reservations[reservationID]
	if reservation.final {
		return nil
	}
	state.reserved -= reservation.estimate
	reservation.final = true
	return nil
}

// Sweep immediately prunes finalized reservations (the same work Reserve does
// lazily every sweepInterval). It is used by shutdown drain and tests.
func (m *Manager) Sweep() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastSweep = time.Time{}
	m.sweepLocked(m.clock.Now())
}
func (m *Manager) Usage(policyID string) (reserved, consumed int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.windows[policyID]
	if state != nil {
		return state.reserved, state.consumed
	}
	return 0, 0
}
