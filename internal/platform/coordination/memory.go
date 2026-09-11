package coordination

import (
	"context"
	"sync"
	"time"
)

// MemoryBudgetLedger is a self-contained single-process implementation of BudgetLedger
// for Lite. Correctness relies on the pipeline's exactly-once finalization; Sweep is a
// no-op because the process owns all state and crash recovery clears it.
type MemoryBudgetLedger struct {
	mu           sync.Mutex
	clock        func() time.Time
	windows      map[string]map[string]float64
	reservations map[string]map[string]*memoryReservation
}

type memoryReservation struct {
	estimate float64
	windows  []string
	final    bool
}

func NewMemoryBudgetLedger(clock func() time.Time) *MemoryBudgetLedger {
	if clock == nil {
		clock = time.Now
	}
	return &MemoryBudgetLedger{
		clock:        clock,
		windows:      make(map[string]map[string]float64),
		reservations: make(map[string]map[string]*memoryReservation),
	}
}

func (m *MemoryBudgetLedger) Reserve(_ context.Context, req ReserveRequest) (ReserveResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if req.ReservationID == "" || req.Estimate < 0 {
		return ReserveResult{}, errorsNew("invalid reserve request")
	}
	tenant := m.tenantWindows(req.TenantID)
	windowKeys := make([]string, 0, len(req.Windows))
	for _, window := range req.Windows {
		if window.Key == "" || window.Limit < 0 {
			return ReserveResult{}, errorsNew("invalid budget window")
		}
		if tenant[window.Key]+req.Estimate > window.Limit {
			return ReserveResult{Reserved: false}, nil
		}
		windowKeys = append(windowKeys, window.Key)
	}
	for _, key := range windowKeys {
		tenant[key] += req.Estimate
	}
	if m.reservations[req.TenantID] == nil {
		m.reservations[req.TenantID] = make(map[string]*memoryReservation)
	}
	m.reservations[req.TenantID][req.ReservationID] = &memoryReservation{estimate: req.Estimate, windows: windowKeys}
	return ReserveResult{Reserved: true}, nil
}

func (m *MemoryBudgetLedger) Reconcile(_ context.Context, tenantID, reservationID string, actual float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	reservation, err := m.reservation(tenantID, reservationID)
	if err != nil {
		return err
	}
	delta := actual - reservation.estimate
	tenant := m.tenantWindows(tenantID)
	for _, window := range reservation.windows {
		tenant[window] += delta
	}
	reservation.final = true
	return nil
}

func (m *MemoryBudgetLedger) Release(_ context.Context, tenantID, reservationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	reservation, err := m.reservation(tenantID, reservationID)
	if err != nil {
		return err
	}
	tenant := m.tenantWindows(tenantID)
	for _, window := range reservation.windows {
		tenant[window] -= reservation.estimate
	}
	reservation.final = true
	return nil
}

func (m *MemoryBudgetLedger) Sweep(_ context.Context, _ string, _ int) (int, error) {
	// No-op for Lite: single-process ownership and exactly-once finalization make
	// sweeping unnecessary; a crashed process clears all in-memory state.
	return 0, nil
}

func (m *MemoryBudgetLedger) tenantWindows(tenantID string) map[string]float64 {
	if m.windows[tenantID] == nil {
		m.windows[tenantID] = make(map[string]float64)
	}
	return m.windows[tenantID]
}

func (m *MemoryBudgetLedger) reservation(tenantID, reservationID string) (*memoryReservation, error) {
	reservations := m.reservations[tenantID]
	if reservations == nil {
		return nil, ErrReservationNotFound
	}
	reservation := reservations[reservationID]
	if reservation == nil {
		return nil, ErrReservationNotFound
	}
	if reservation.final {
		return nil, ErrAlreadyFinalized
	}
	return reservation, nil
}

func errorsNew(message string) error {
	return &coordinationError{message: message}
}

type coordinationError struct{ message string }

func (e *coordinationError) Error() string { return e.message }

// MemoryLeaseCoordinator is a self-contained single-process LeaseCoordinator.
type MemoryLeaseCoordinator struct {
	mu     sync.Mutex
	clock  func() time.Time
	scopes map[string]map[string]time.Time
}

func NewMemoryLeaseCoordinator(clock func() time.Time) *MemoryLeaseCoordinator {
	if clock == nil {
		clock = time.Now
	}
	return &MemoryLeaseCoordinator{clock: clock, scopes: make(map[string]map[string]time.Time)}
}

func (m *MemoryLeaseCoordinator) Acquire(_ context.Context, scope string, capacity int, ttl time.Duration) (bool, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.clock()
	leases := m.leases(scope)
	m.prune(leases, now)
	if len(leases) >= capacity {
		return false, "", nil
	}
	id, err := newLeaseID()
	if err != nil {
		return false, "", err
	}
	leases[id] = now.Add(ttl)
	return true, id, nil
}

func (m *MemoryLeaseCoordinator) Renew(_ context.Context, scope, leaseID string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.clock()
	leases := m.leases(scope)
	m.prune(leases, now)
	if _, ok := leases[leaseID]; !ok {
		return false, nil
	}
	leases[leaseID] = now.Add(ttl)
	return true, nil
}

func (m *MemoryLeaseCoordinator) Release(_ context.Context, scope, leaseID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.leases(scope), leaseID)
	return nil
}

func (m *MemoryLeaseCoordinator) Cleanup(_ context.Context, scope string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune(m.leases(scope), m.clock())
	return nil
}

func (m *MemoryLeaseCoordinator) leases(scope string) map[string]time.Time {
	if m.scopes[scope] == nil {
		m.scopes[scope] = make(map[string]time.Time)
	}
	return m.scopes[scope]
}

func (m *MemoryLeaseCoordinator) prune(leases map[string]time.Time, now time.Time) {
	for id, expiry := range leases {
		if !expiry.After(now) {
			delete(leases, id)
		}
	}
}

// MemoryInflightCounter is a self-contained InflightCounter.
type MemoryInflightCounter struct {
	mu    sync.Mutex
	value map[string]int64
}

func NewMemoryInflightCounter() *MemoryInflightCounter {
	return &MemoryInflightCounter{value: make(map[string]int64)}
}

func (m *MemoryInflightCounter) Incr(_ context.Context, scope string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.value[scope]++
	return m.value[scope], nil
}

func (m *MemoryInflightCounter) Decr(_ context.Context, scope string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.value[scope] > 0 {
		m.value[scope]--
	}
	return nil
}

func (m *MemoryInflightCounter) Get(_ context.Context, scope string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.value[scope], nil
}
