// Package federation owns cross-organization Agent trust: relationships,
// verified trust anchors, project/capability grants, data boundaries, and the
// discovery≠trust lifecycle. LiteAIG only relies on local, provable facts.
package federation

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/F31/liteAIG/internal/tenancy"
)

// Status is the relationship lifecycle state.
type Status string

const (
	StatusDiscovered    Status = "discovered"
	StatusCandidate     Status = "candidate"
	StatusPendingReview Status = "pending_review"
	StatusActive        Status = "active"
	StatusSuspended     Status = "suspended"
	StatusRevoked       Status = "revoked"
)

// AnchorType enumerates verified trust anchor kinds.
type AnchorType string

const (
	AnchorJWS      AnchorType = "jws"
	AnchorMTLS     AnchorType = "mtls_spki"
	AnchorOIDC     AnchorType = "oidc"
	AnchorRegistry AnchorType = "registry_attestation"
)

// DataBoundaryStatus classifies external data handling facts.
type DataBoundaryStatus string

const (
	BoundaryUnknown            DataBoundaryStatus = "unknown"
	BoundaryDeclared           DataBoundaryStatus = "declared"
	BoundaryContractuallyBound DataBoundaryStatus = "contractually_bound"
)

// TrustAnchor is one verified credential root for a relationship.
type TrustAnchor struct {
	ID         string
	Type       AnchorType
	Subject    string
	Verified   bool
	VerifiedAt time.Time
}

// DataBoundary describes how an external agent handles data.
type DataBoundary struct {
	Status            DataBoundaryStatus
	ProcessingRegions []string
	Retention         string
	TrainingUse       bool
	Subprocessors     []string
	DPAReference      string
}

// ProjectGrant authorizes a Project to call the external agent.
type ProjectGrant struct {
	ID, ProjectID, GrantedBy string
	CreatedAt                time.Time
}

// CapabilityGrant authorizes a capability exposure.
type CapabilityGrant struct {
	ID, Capability string
	CreatedAt      time.Time
}

// Relationship is a tenant-scoped federated agent trust relationship.
type Relationship struct {
	ID               string
	TenantID         string
	ExternalAgentID  string
	ExternalSubject  string
	Name             string
	Status           Status
	Direction        string // outbound|inbound|bidirectional
	AssuranceLevel   string
	AgentCardSource  string
	AuthMethod       string
	DataBoundary     DataBoundary
	Anchors          []TrustAnchor
	ProjectGrants    []ProjectGrant
	CapabilityGrants []CapabilityGrant
	PricingRef       string
	ApprovedVersion  string
	ReviewStatus     string
	Version          int64 // optimistic-concurrency guard, bumped on every save
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// HasVerifiedAnchor reports whether at least one trust anchor is verified.
func (r *Relationship) HasVerifiedAnchor() bool {
	for _, anchor := range r.Anchors {
		if anchor.Verified {
			return true
		}
	}
	return false
}

var (
	ErrNotActive     = errors.New("federation relationship is not active")
	ErrUnverified    = errors.New("federation relationship has no verified trust anchor")
	ErrNotGranted    = errors.New("project or capability grant missing")
	ErrDataBoundary  = errors.New("external data boundary does not satisfy policy")
	ErrAlreadyActive = errors.New("federation relationship already active")
	ErrStaleState    = errors.New("federation relationship is in a non-transitional state")
)

// Store persists relationships so lifecycle state survives restarts.
type Store interface {
	Put(context.Context, Relationship) error
	Get(context.Context, tenancy.TenantScope, string) (Relationship, bool)
	List(context.Context, tenancy.TenantScope) ([]Relationship, error)
}

type memoryStore map[string]Relationship

func (m memoryStore) Put(_ context.Context, r Relationship) error {
	if m == nil {
		return nil
	}
	m[r.ID] = r
	return nil
}
func (m memoryStore) Get(_ context.Context, scope tenancy.TenantScope, id string) (Relationship, bool) {
	if m == nil {
		return Relationship{}, false
	}
	r, ok := m[id]
	if ok && r.TenantID != scope.TenantID {
		return Relationship{}, false
	}
	return r, ok
}
func (m memoryStore) List(_ context.Context, scope tenancy.TenantScope) ([]Relationship, error) {
	var result []Relationship
	for _, r := range m {
		if r.TenantID == scope.TenantID {
			result = append(result, r)
		}
	}
	return result, nil
}

// Lifecycle transitions a relationship between states and records the change.
type Lifecycle struct {
	mu     sync.Mutex
	states map[string]Relationship
	store  Store
	now    func() time.Time
}

// NewLifecycle builds an in-memory relationship store with lifecycle rules.
func NewLifecycle(now func() time.Time) *Lifecycle {
	return NewLifecycleWithStore(now, nil)
}

// NewLifecycleWithStore builds a Lifecycle persisted through store (in-memory
// when store is nil).
func NewLifecycleWithStore(now func() time.Time, store Store) *Lifecycle {
	if now == nil {
		now = time.Now
	}
	if store == nil {
		store = memoryStore{}
	}
	return &Lifecycle{states: map[string]Relationship{}, store: store, now: now}
}

// Get returns a relationship by id, falling back to the store.
func (l *Lifecycle) Get(ctx context.Context, scope tenancy.TenantScope, id string) (Relationship, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.loadLocked(ctx, scope, id)
}

// List returns every relationship in the tenant scope.
func (l *Lifecycle) List(ctx context.Context, scope tenancy.TenantScope) ([]Relationship, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.store.List(ctx, scope)
}

// loadLocked reads from the hot map, then the backing store.
func (l *Lifecycle) loadLocked(ctx context.Context, scope tenancy.TenantScope, id string) (Relationship, bool) {
	if relationship, ok := l.states[id]; ok {
		if relationship.TenantID != scope.TenantID {
			return Relationship{}, false
		}
		return relationship, true
	}
	relationship, ok := l.store.Get(ctx, scope, id)
	if ok {
		l.states[id] = relationship
	}
	return relationship, ok
}

// saveLocked writes the hot map and the backing store.
func (l *Lifecycle) saveLocked(relationship Relationship) error {
	l.states[relationship.ID] = relationship
	return l.store.Put(context.Background(), relationship)
}

// Discover creates a candidate; discovery never implies trust.
func (l *Lifecycle) Discover(ctx context.Context, relationship Relationship) (Relationship, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	relationship.Status = StatusCandidate
	relationship.UpdatedAt = l.now().UTC()
	relationship.Version = 1
	if _, exists := l.loadLocked(ctx, tenancy.TenantScope{TenantID: relationship.TenantID}, relationship.ID); exists {
		return relationship, ErrAlreadyActive
	}
	if err := l.saveLocked(relationship); err != nil {
		return relationship, err
	}
	return relationship, nil
}

// Review moves a candidate to pending_review or active once requirements pass.
func (l *Lifecycle) Review(ctx context.Context, relationship Relationship, approved bool) (Relationship, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	scope := tenancy.TenantScope{TenantID: relationship.TenantID}
	current, ok := l.loadLocked(ctx, scope, relationship.ID)
	if !ok {
		return relationship, errors.New("relationship not found")
	}
	if current.Status != StatusCandidate && current.Status != StatusPendingReview {
		return current, ErrStaleState
	}
	if !approved {
		current.Status = StatusPendingReview
		current.ReviewStatus = "rejected"
		current.UpdatedAt = l.now().UTC()
		current.Version++
		if err := l.saveLocked(current); err != nil {
			return current, err
		}
		return current, nil
	}
	// The caller's copy must be based on the latest stored state; otherwise a
	// concurrent transition (suspend, material change) would be silently lost.
	if relationship.Version != current.Version {
		return current, ErrStaleState
	}
	relationship.Status = StatusActive
	relationship.ReviewStatus = "approved"
	relationship.UpdatedAt = l.now().UTC()
	relationship.Version = current.Version + 1
	if err := l.saveLocked(relationship); err != nil {
		return relationship, err
	}
	return relationship, nil
}

// Activate requires a verified anchor plus project and capability grants. The
// caller's copy must be based on the latest stored state (Version matches),
// so a stale re-activation cannot clobber a concurrent transition.
func (l *Lifecycle) Activate(ctx context.Context, relationship Relationship) (Relationship, error) {
	if !relationship.HasVerifiedAnchor() {
		return relationship, ErrUnverified
	}
	if len(relationship.ProjectGrants) == 0 || len(relationship.CapabilityGrants) == 0 {
		return relationship, ErrNotGranted
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	current, exists := l.loadLocked(ctx, tenancy.TenantScope{TenantID: relationship.TenantID}, relationship.ID)
	if exists && relationship.Version != current.Version {
		return current, ErrStaleState
	}
	relationship.Status = StatusActive
	relationship.ReviewStatus = "approved"
	relationship.UpdatedAt = l.now().UTC()
	if exists {
		relationship.Version = current.Version + 1
	} else {
		relationship.Version = 1
	}
	if err := l.saveLocked(relationship); err != nil {
		return relationship, err
	}
	return relationship, nil
}

// Suspend blocks new calls; Revoke terminates the relationship.
func (l *Lifecycle) Suspend(ctx context.Context, scope tenancy.TenantScope, id string) (Relationship, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	relationship, ok := l.loadLocked(ctx, scope, id)
	if !ok {
		return relationship, errors.New("relationship not found")
	}
	if relationship.Status == StatusRevoked {
		return relationship, ErrStaleState
	}
	relationship.Status = StatusSuspended
	relationship.UpdatedAt = l.now().UTC()
	relationship.Version++
	if err := l.saveLocked(relationship); err != nil {
		return relationship, err
	}
	return relationship, nil
}

// Revoke terminates a relationship.
func (l *Lifecycle) Revoke(ctx context.Context, scope tenancy.TenantScope, id string) (Relationship, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	relationship, ok := l.loadLocked(ctx, scope, id)
	if !ok {
		return relationship, errors.New("relationship not found")
	}
	relationship.Status = StatusRevoked
	relationship.ReviewStatus = "revoked"
	relationship.UpdatedAt = l.now().UTC()
	relationship.Version++
	if err := l.saveLocked(relationship); err != nil {
		return relationship, err
	}
	return relationship, nil
}

// Active reports whether a relationship may serve new external calls.
func (l *Lifecycle) Active(scope tenancy.TenantScope, id string) bool {
	relationship, ok := l.Get(context.Background(), scope, id)
	return ok && relationship.Status == StatusActive
}

// MaterialChange describes a changed Agent Card fact that requires review.
type MaterialChange struct {
	Fields []string // endpoint|auth_scheme|capability|publisher|trust_key|data_boundary|pricing
}

// RequiresReview reports whether any listed field is material.
func (m MaterialChange) RequiresReview() bool {
	return len(m.Fields) > 0
}

// ReviewMaterialChange transitions an active relationship to pending_review when
// a material Agent Card change is detected. It never auto-activates new facts.
func (l *Lifecycle) ReviewMaterialChange(ctx context.Context, scope tenancy.TenantScope, id string, change MaterialChange) (Relationship, error) {
	if !change.RequiresReview() {
		relationship, ok := l.Get(ctx, scope, id)
		if !ok {
			return relationship, errors.New("relationship not found")
		}
		return relationship, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	relationship, ok := l.loadLocked(ctx, scope, id)
	if !ok {
		return relationship, errors.New("relationship not found")
	}
	relationship.Status = StatusPendingReview
	relationship.ReviewStatus = "material_change:" + change.Fields[0]
	relationship.UpdatedAt = l.now().UTC()
	relationship.Version++
	if err := l.saveLocked(relationship); err != nil {
		return relationship, err
	}
	return relationship, nil
}
