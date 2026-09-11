// Package circuit implements the complete per-provider/deployment/credential
// circuit state machine with escalating cooldown, exclusive half-open probes,
// drift-triggered resets, and durable restart reconstruction.
package circuit

import (
	"context"
	"sync"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
)

const (
	StateClosed   = "closed"
	StateOpen     = "open"
	StateHalfOpen = "half_open"
	StateReset    = "reset"
)

type Config struct {
	MinSamples     int
	ErrorRate      float64
	Cooldown       time.Duration // initial cooldown
	CooldownFactor float64
	MaxCooldown    time.Duration
	Store          Store
	Observer       func(Transition)
}

func DefaultConfig() Config {
	return Config{MinSamples: 20, ErrorRate: .5, Cooldown: 30 * time.Second, CooldownFactor: 2, MaxCooldown: 10 * time.Minute}
}

type Transition struct {
	DeploymentID, CredentialID, From, To string
	Reason                               string
	At                                   time.Time
}

// TupleKey identifies one circuit instance.
type TupleKey struct {
	DeploymentID string
	CredentialID string
}

// Fact is the durable circuit state used to reconstruct after restart.
type Fact struct {
	DeploymentID  string
	CredentialID  string
	State         string
	OpenedAtMS    int64
	LastProbeAtMS int64
	SampleCount   int64
	FailureCount  int64
	OpenCount     int64
}

// Store persists durable circuit facts after decisions.
type Store interface {
	Load(context.Context) ([]Fact, error)
	Save(context.Context, Fact) error
	Delete(context.Context, TupleKey) error
}

type entry struct {
	mu          sync.Mutex
	state       string
	samples     int
	failures    int
	openedAt    time.Time
	lastProbeAt time.Time
	probeTaken  bool
	openCount   int
}

type Breaker struct {
	mu      sync.RWMutex
	config  Config
	clock   contracts.Clock
	entries sync.Map
}

func New(config Config, clock contracts.Clock) *Breaker {
	return &Breaker{config: config, clock: clock}
}

// cfg returns a consistent copy of the current configuration.
func (b *Breaker) cfg() Config {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.config
}

// SetConfig atomically swaps the circuit parameters (e.g. from a newly
// published tenant snapshot). In-flight decisions keep the previous config.
func (b *Breaker) SetConfig(config Config) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config = config
}

// Start reconstructs state from the durable store, when configured.
func (b *Breaker) Start(ctx context.Context) error {
	cfg := b.cfg()
	if cfg.Store == nil {
		return nil
	}
	facts, err := cfg.Store.Load(ctx)
	if err != nil {
		return err
	}
	for _, fact := range facts {
		item := b.get(fact.DeploymentID, fact.CredentialID)
		item.mu.Lock()
		item.state = fact.State
		item.samples = int(fact.SampleCount)
		item.failures = int(fact.FailureCount)
		item.openCount = int(fact.OpenCount)
		item.openedAt = time.UnixMilli(fact.OpenedAtMS)
		item.lastProbeAt = time.UnixMilli(fact.LastProbeAtMS)
		item.mu.Unlock()
	}
	return nil
}

func (b *Breaker) get(deploymentID, credentialID string) *entry {
	value, _ := b.entries.LoadOrStore(deploymentID+":"+credentialID, &entry{state: StateClosed})
	return value.(*entry)
}

func (b *Breaker) cooldown(openCount int) time.Duration {
	if openCount <= 0 {
		openCount = 1
	}
	cfg := b.cfg()
	cooldown := cfg.Cooldown
	if cfg.CooldownFactor > 1 {
		for i := 1; i < openCount; i++ {
			cooldown = time.Duration(float64(cooldown) * cfg.CooldownFactor)
		}
	}
	if cfg.MaxCooldown > 0 && cooldown > cfg.MaxCooldown {
		cooldown = cfg.MaxCooldown
	}
	if cooldown <= 0 {
		cooldown = cfg.Cooldown
	}
	return cooldown
}

// Open reports whether the tuple should be excluded from routing.
func (b *Breaker) Open(deploymentID, credentialID string) bool {
	item := b.get(deploymentID, credentialID)
	item.mu.Lock()
	defer item.mu.Unlock()
	return b.openNow(item)
}

func (b *Breaker) openNow(item *entry) bool {
	if item.state != StateOpen {
		return false
	}
	return b.clock.Now().Sub(item.openedAt) < b.cooldown(item.openCount)
}

// Allow decides whether an execution attempt may proceed. Exactly one probe is
// admitted per half-open window.
func (b *Breaker) Allow(deploymentID, credentialID string) bool {
	item := b.get(deploymentID, credentialID)
	item.mu.Lock()
	defer item.mu.Unlock()
	switch item.state {
	case StateClosed:
		return true
	case StateOpen:
		if b.clock.Now().Sub(item.openedAt) < b.cooldown(item.openCount) {
			return false
		}
		b.transitionLocked(deploymentID, credentialID, StateHalfOpen, "cooldown_elapsed")
		item.lastProbeAt = b.clock.Now()
		item.probeTaken = true
		return true
	default: // half_open
		if item.probeTaken {
			return false
		}
		item.lastProbeAt = b.clock.Now()
		item.probeTaken = true
		return true
	}
}

func (b *Breaker) Record(deploymentID, credentialID string, success bool) {
	item := b.get(deploymentID, credentialID)
	item.mu.Lock()
	defer item.mu.Unlock()
	switch item.state {
	case StateHalfOpen:
		if success {
			item.state = StateClosed
			item.samples, item.failures = 0, 0
			item.openCount = 0
			item.probeTaken = false
			b.notifyLocked(deploymentID, credentialID, StateHalfOpen, StateClosed, "")
			return
		}
		item.openCount++
		item.openedAt = b.clock.Now()
		item.probeTaken = false
		item.state = StateOpen
		b.notifyLocked(deploymentID, credentialID, StateHalfOpen, StateOpen, "")
		return
	case StateOpen:
		return
	default:
		cfg := b.cfg()
		item.samples++
		if !success {
			item.failures++
		}
		if item.samples >= cfg.MinSamples && float64(item.failures)/float64(item.samples) > cfg.ErrorRate {
			item.openCount = 1
			item.openedAt = b.clock.Now()
			item.probeTaken = false
			item.state = StateOpen
			b.notifyLocked(deploymentID, credentialID, StateClosed, StateOpen, "")
		}
	}
}

// Reset deterministically clears a tuple (used on configuration drift).
func (b *Breaker) Reset(deploymentID, credentialID string) {
	item := b.get(deploymentID, credentialID)
	item.mu.Lock()
	from := item.state
	item.state = StateClosed
	item.samples, item.failures = 0, 0
	item.openCount = 0
	item.probeTaken = false
	item.openedAt = time.Time{}
	item.lastProbeAt = time.Time{}
	item.mu.Unlock()
	b.entries.Delete(deploymentID + ":" + credentialID)
	if store := b.cfg().Store; store != nil {
		_ = store.Delete(context.Background(), TupleKey{DeploymentID: deploymentID, CredentialID: credentialID})
	}
	if from != StateClosed {
		b.notify(&Transition{DeploymentID: deploymentID, CredentialID: credentialID, From: from, To: StateReset, Reason: "drift", At: b.clock.Now()})
	}
}

// transitionLocked changes state and persists/notifies while holding the lock.
func (b *Breaker) transitionLocked(deploymentID, credentialID, to, reason string) {
	item := b.get(deploymentID, credentialID)
	from := item.state
	item.state = to
	b.persistLocked(deploymentID, credentialID)
	if from != to {
		b.notify(&Transition{DeploymentID: deploymentID, CredentialID: credentialID, From: from, To: to, Reason: reason, At: b.clock.Now()})
	}
}

func (b *Breaker) notifyLocked(deploymentID, credentialID, from, to, reason string) {
	b.persistLocked(deploymentID, credentialID)
	b.notify(&Transition{DeploymentID: deploymentID, CredentialID: credentialID, From: from, To: to, Reason: reason, At: b.clock.Now()})
}

func (b *Breaker) persistLocked(deploymentID, credentialID string) {
	store := b.cfg().Store
	if store == nil {
		return
	}
	item := b.get(deploymentID, credentialID)
	_ = store.Save(context.Background(), Fact{
		DeploymentID: deploymentID, CredentialID: credentialID, State: item.state,
		OpenedAtMS: item.openedAt.UnixMilli(), LastProbeAtMS: item.lastProbeAt.UnixMilli(),
		SampleCount: int64(item.samples), FailureCount: int64(item.failures), OpenCount: int64(item.openCount),
	})
}

func (b *Breaker) notify(value *Transition) {
	if observer := b.cfg().Observer; observer != nil {
		observer(*value)
	}
}
