// Package approval owns the human approval lifecycle with Separation of Duties:
// a requester can never approve their own request, and Enterprise dual approval
// requires two distinct non-requester approvers.
package approval

import (
	"context"
	"errors"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/tenancy"
)

// Status is the approval lifecycle state.
type Status string

const (
	StatusPending   Status = "pending"
	StatusApproved  Status = "approved"
	StatusRejected  Status = "rejected"
	StatusCancelled Status = "cancelled"
)

var (
	ErrSelfApproval    = errors.New("requester cannot approve their own request")
	ErrNotPending      = errors.New("approval is not pending")
	ErrDualRequired    = errors.New("dual approval requires a second distinct approver")
	ErrNotFound        = errors.New("approval not found")
	ErrScopeMismatch   = errors.New("approval tenant scope mismatch")
	ErrUnknownApprover = errors.New("approver is not an active local user")
)

// Request is a high-risk action awaiting human approval.
type Request struct {
	ID           string
	TenantID     string
	Requester    string
	Action       string
	Target       string
	DualApproval bool
	Status       Status
	ApprovedBy   []string
	RejectedBy   string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Service persists approvals and enforces SoD + dual approval.
type Service struct {
	store            Store
	events           contracts.EventSink
	now              func() time.Time
	approverVerifier func(ctx context.Context, scope tenancy.TenantScope, approver string) error
}

// SetEventSink installs best-effort lifecycle notifications before serving traffic.
func (s *Service) SetEventSink(events contracts.EventSink) { s.events = events }

func (s *Service) emit(ctx context.Context, request Request, kind string) {
	if s.events != nil {
		_ = s.events.Emit(ctx, contracts.DomainEvent{ID: request.ID, Kind: kind, OccurredAt: request.UpdatedAt,
			TenantID: request.TenantID, Attributes: map[string]string{"status": string(request.Status)}})
	}
}

// Store is the tenant-scoped approval persistence contract.
type Store interface {
	Create(context.Context, tenancy.TenantScope, Request) error
	Get(context.Context, tenancy.TenantScope, string) (*Request, error)
	Update(context.Context, tenancy.TenantScope, Request) error
	List(context.Context, tenancy.TenantScope, int) ([]Request, error)
	// Find returns the most recent request for (target, requester) in any
	// status, so the data plane can resolve approval state idempotently.
	Find(context.Context, tenancy.TenantScope, string, string) (*Request, error)
}

// NewService builds an approval service.
func NewService(store Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, now: now}
}

// SetApproverVerifier installs an identity check run before any decision is
// recorded: the actor must resolve to an active local user of the tenant.
// Without a verifier the service cannot distinguish forged actor IDs, so the
// app layer must wire one in any deployment where approvals gate actions.
func (s *Service) SetApproverVerifier(fn func(ctx context.Context, scope tenancy.TenantScope, approver string) error) {
	s.approverVerifier = fn
}

func (s *Service) verifyApprover(ctx context.Context, scope tenancy.TenantScope, approver string) error {
	if s.approverVerifier == nil {
		return nil
	}
	return s.approverVerifier(ctx, scope, approver)
}

// Create opens a pending approval request.
func (s *Service) Create(ctx context.Context, scope tenancy.TenantScope, request Request) (*Request, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if request.Requester == "" || request.Action == "" {
		return nil, errors.New("approval requester and action are required")
	}
	request.TenantID = scope.TenantID
	request.Status = StatusPending
	request.CreatedAt = s.now().UTC()
	request.UpdatedAt = request.CreatedAt
	if err := s.store.Create(ctx, scope, request); err != nil {
		return nil, err
	}
	s.emit(ctx, request, "approval.created")
	return &request, nil
}

// Approve applies an approval decision with SoD and dual-approval enforcement.
func (s *Service) Approve(ctx context.Context, scope tenancy.TenantScope, id, approver string) (*Request, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	request, err := s.store.Get(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	if request.Status != StatusPending {
		return nil, ErrNotPending
	}
	if err := s.verifyApprover(ctx, scope, approver); err != nil {
		return nil, err
	}
	if approver == request.Requester {
		return nil, ErrSelfApproval
	}
	for _, existing := range request.ApprovedBy {
		if existing == approver {
			return nil, errors.New("approver already decided")
		}
	}
	request.ApprovedBy = append(request.ApprovedBy, approver)
	if request.DualApproval && len(request.ApprovedBy) < 2 {
		request.UpdatedAt = s.now().UTC()
		if err := s.store.Update(ctx, scope, *request); err != nil {
			return nil, err
		}
		s.emit(ctx, *request, "approval.partial")
		return request, nil // still pending; needs a second approver
	}
	request.Status = StatusApproved
	request.UpdatedAt = s.now().UTC()
	if err := s.store.Update(ctx, scope, *request); err != nil {
		return nil, err
	}
	s.emit(ctx, *request, "approval.approved")
	return request, nil
}

// Reject denies a pending request.
func (s *Service) Reject(ctx context.Context, scope tenancy.TenantScope, id, reviewer string) (*Request, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	request, err := s.store.Get(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	if request.Status != StatusPending {
		return nil, ErrNotPending
	}
	if err := s.verifyApprover(ctx, scope, reviewer); err != nil {
		return nil, err
	}
	if reviewer == request.Requester {
		return nil, ErrSelfApproval
	}
	request.Status = StatusRejected
	request.RejectedBy = reviewer
	request.UpdatedAt = s.now().UTC()
	if err := s.store.Update(ctx, scope, *request); err != nil {
		return nil, err
	}
	s.emit(ctx, *request, "approval.rejected")
	return request, nil
}

// List returns the tenant's recent approval requests.
func (s *Service) List(ctx context.Context, scope tenancy.TenantScope, limit int) ([]Request, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return s.store.List(ctx, scope, limit)
}

// Find returns the most recent request for (target, requester) in any status.
func (s *Service) Find(ctx context.Context, scope tenancy.TenantScope, target, requester string) (*Request, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return s.store.Find(ctx, scope, target, requester)
}
