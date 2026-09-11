// Package rate implements the Phase 0 process-local scoped RPM limiter and
// the P0 TPM (tokens-per-minute) meter with estimate-then-reconcile.
package rate

import (
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"sync"
	"time"
)

type Policy struct{ RequestsPerMinute, Burst int }
type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
}
type bucket struct {
	tokens float64
	last   time.Time
}

const (
	// sweepInterval amortizes the eviction scan: at most one O(buckets) pass
	// per interval per shard across all callers.
	sweepInterval = 60 * time.Second
	// idleTTL bounds how long an unused (tenant, project) bucket is kept. An
	// idle bucket keeps earning its full burst after this point, so eviction
	// costs at most one extra burst per evicted scope.
	idleTTL = 10 * time.Minute
	// shardCount spreads scopes over independent lock shards so the hot path
	// never serializes all tenants on one mutex.
	shardCount = 16
)

type shard struct {
	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

type Limiter struct {
	clock  contracts.Clock
	shards [shardCount]*shard
}

func New(clock contracts.Clock) *Limiter {
	l := &Limiter{clock: clock}
	for i := range l.shards {
		l.shards[i] = &shard{buckets: make(map[string]*bucket), lastSweep: clock.Now()}
	}
	return l
}

// shardIndex maps a scope key to its shard with FNV-1a.
func shardIndex(key string) int {
	var h uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return int(h % uint32(shardCount))
}

// sweepLocked evicts idle buckets so the map does not grow with every
// (tenant, project) pair ever seen. Callers must hold s.mu.
func (s *shard) sweepLocked(now time.Time) {
	if now.Sub(s.lastSweep) < sweepInterval {
		return
	}
	s.lastSweep = now
	for key, state := range s.buckets {
		if now.Sub(state.last) >= idleTTL {
			delete(s.buckets, key)
		}
	}
}

// sweepDue evicts idle buckets in every shard whose sweep interval elapsed.
// Each shard is visited under its own lock; the scan stays amortized at one
// O(buckets) pass per shard per interval.
func (l *Limiter) sweepDue(now time.Time) {
	for _, s := range l.shards {
		s.mu.Lock()
		s.sweepLocked(now)
		s.mu.Unlock()
	}
}

// bucketExists reports whether a scope bucket is currently held.
func (l *Limiter) bucketExists(key string) bool {
	s := l.shards[shardIndex(key)]
	s.mu.Lock()
	defer s.mu.Unlock()
	_, exists := s.buckets[key]
	return exists
}

func (l *Limiter) Allow(tenantID, projectID string, policy Policy) Decision {
	if policy.RequestsPerMinute <= 0 || policy.Burst <= 0 {
		return Decision{Allowed: false, RetryAfter: time.Minute}
	}
	key := tenantID + ":" + projectID
	now := l.clock.Now()
	l.sweepDue(now)
	s := l.shards[shardIndex(key)]
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.buckets[key]
	if state == nil {
		state = &bucket{tokens: float64(policy.Burst), last: now}
		s.buckets[key] = state
	}
	elapsed := now.Sub(state.last).Minutes()
	if elapsed < 0 {
		elapsed = 0
	}
	state.tokens = min(float64(policy.Burst), state.tokens+elapsed*float64(policy.RequestsPerMinute))
	state.last = now
	if state.tokens >= 1 {
		state.tokens--
		return Decision{Allowed: true}
	}
	missing := 1 - state.tokens
	return Decision{RetryAfter: time.Duration(missing / float64(policy.RequestsPerMinute) * float64(time.Minute))}
}

// TPM meter

// TPMPolicy caps tokens per fixed minute window per scope. TokensPerMinute of
// 0 or negative disables the meter for that scope.
type TPMPolicy struct{ TokensPerMinute int64 }

// TPMPolicyFunc returns the TPM cap for a scope, or zero to disable.
type TPMPolicyFunc func(tenantID, projectID string) int64

// TPMDecision is the outcome of a TPM pre-check. RetryAfter is the wait until
// the window rolls over when Allowed is false.
type TPMDecision struct {
	Allowed    bool
	RetryAfter time.Duration
}

// tpmBucket tracks the fixed-window token ledger for one scope.
type tpmBucket struct {
	window time.Time
	used   int64
}

// Meter is the fixed-window TPM meter. It follows the spec §14.5: requests
// pre-reserve tokens by estimate before the provider call and reconcile to the
// actual usage when the response lands, so concurrent callers cannot overshoot
// the window budget.
type Meter struct {
	clock  contracts.Clock
	policy TPMPolicyFunc
	mu     sync.Mutex
	scopes map[string]*tpmBucket
}

func NewMeter(clock contracts.Clock, policy TPMPolicyFunc) *Meter {
	return &Meter{clock: clock, policy: policy, scopes: make(map[string]*tpmBucket)}
}

func (m *Meter) scopeKey(tenantID, projectID string) string {
	return tenantID + ":" + projectID
}

// Reserve pre-reserves estimated tokens against the current minute window.
// The returned reservation is required to reconcile final usage.
func (m *Meter) Reserve(tenantID, projectID string, estimated int64) (TPMDecision, int) {
	limit := int64(0)
	if m.policy != nil {
		limit = m.policy(tenantID, projectID)
	}
	if limit <= 0 || estimated <= 0 {
		return TPMDecision{Allowed: true}, -1
	}
	now := m.clock.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	key := m.scopeKey(tenantID, projectID)
	state := m.scopes[key]
	if state == nil || !sameMinute(state.window, now) {
		state = &tpmBucket{window: now}
		m.scopes[key] = state
	}
	if state.used+estimated > limit {
		return TPMDecision{Allowed: false, RetryAfter: nextMinute(now).Sub(now)}, -1
	}
	state.used += estimated
	return TPMDecision{Allowed: true}, len(m.scopes)
}

// Reconcile adjusts the reserved tokens to the actual usage so the meter
// reflects real consumption. estimated is what Reserve pre-reserved.
func (m *Meter) Reconcile(tenantID, projectID string, estimated, actual int64) {
	if estimated <= 0 && actual <= 0 {
		return
	}
	now := m.clock.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	key := m.scopeKey(tenantID, projectID)
	state := m.scopes[key]
	if state == nil || !sameMinute(state.window, now) {
		// The reservation was never recorded or the window rolled over; the
		// actual usage is not charged to a stale ledger.
		return
	}
	state.used -= estimated
	state.used += actual
	if state.used < 0 {
		state.used = 0
	}
}

// windowState reports the current window usage for observability.
func (m *Meter) windowState(tenantID, projectID string) (window time.Time, used, limit int64, active bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	limit = 0
	if m.policy != nil {
		limit = m.policy(tenantID, projectID)
	}
	state := m.scopes[m.scopeKey(tenantID, projectID)]
	if state == nil {
		return time.Time{}, 0, limit, false
	}
	return state.window, state.used, limit, true
}

func sameMinute(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay() && a.Hour() == b.Hour() && a.Minute() == b.Minute()
}

func nextMinute(now time.Time) time.Time {
	return now.Truncate(time.Minute).Add(time.Minute)
}
