package approval

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/tenancy"
)

type memStore struct{ items map[string]*Request }

type failingEventSink struct{ events []contracts.DomainEvent }

func (s *failingEventSink) Emit(_ context.Context, event contracts.DomainEvent) error {
	s.events = append(s.events, event)
	return errors.New("notification unavailable")
}

func newMemStore() *memStore { return &memStore{items: map[string]*Request{}} }

func (m *memStore) Create(_ context.Context, scope tenancy.TenantScope, request Request) error {
	if request.TenantID != scope.TenantID {
		return ErrScopeMismatch
	}
	copy := request
	m.items[request.ID] = &copy
	return nil
}
func (m *memStore) Get(_ context.Context, scope tenancy.TenantScope, id string) (*Request, error) {
	request, ok := m.items[id]
	if !ok || request.TenantID != scope.TenantID {
		return nil, ErrNotFound
	}
	return request, nil
}
func (m *memStore) Update(_ context.Context, scope tenancy.TenantScope, request Request) error {
	if request.TenantID != scope.TenantID {
		return ErrScopeMismatch
	}
	copy := request
	m.items[request.ID] = &copy
	return nil
}
func (m *memStore) Find(_ context.Context, scope tenancy.TenantScope, target, requester string) (*Request, error) {
	var latest *Request
	for _, item := range m.items {
		if item.TenantID != scope.TenantID || item.Target != target || item.Requester != requester {
			continue
		}
		if latest == nil || item.CreatedAt.After(latest.CreatedAt) {
			latest = item
		}
	}
	if latest == nil {
		return nil, ErrNotFound
	}
	return latest, nil
}
func (m *memStore) List(_ context.Context, scope tenancy.TenantScope, limit int) ([]Request, error) {
	var result []Request
	for _, item := range m.items {
		if item.TenantID == scope.TenantID {
			result = append(result, *item)
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func scope() tenancy.TenantScope { return tenancy.TenantScope{TenantID: "tenant"} }

func TestRequesterCannotSelfApprove(t *testing.T) {
	service := NewService(newMemStore(), func() time.Time { return time.Unix(1, 0) })
	ctx := context.Background()
	request, err := service.Create(ctx, scope(), Request{ID: "a1", Requester: "alice", Action: "external_agent.review", Target: "rel-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(ctx, scope(), request.ID, "alice"); !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("self-approval error = %v", err)
	}
	if _, err := service.Reject(ctx, scope(), request.ID, "alice"); !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("self-reject error = %v", err)
	}
}

func TestDualApprovalRequiresTwoDistinctApprovers(t *testing.T) {
	service := NewService(newMemStore(), func() time.Time { return time.Unix(1, 0) })
	events := &failingEventSink{}
	service.SetEventSink(events)
	ctx := context.Background()
	request, err := service.Create(ctx, scope(), Request{ID: "a2", Requester: "alice", Action: "external_agent.review", Target: "rel-1", DualApproval: true})
	if err != nil {
		t.Fatal(err)
	}
	// First approver keeps it pending.
	afterFirst, err := service.Approve(ctx, scope(), request.ID, "bob")
	if err != nil || afterFirst.Status != StatusPending {
		t.Fatalf("first approve = %+v, %v", afterFirst, err)
	}
	// Same approver again is rejected.
	if _, err := service.Approve(ctx, scope(), request.ID, "bob"); err == nil {
		t.Fatal("duplicate approver accepted")
	}
	// Second distinct approver approves.
	afterSecond, err := service.Approve(ctx, scope(), request.ID, "carol")
	if err != nil || afterSecond.Status != StatusApproved {
		t.Fatalf("second approve = %+v, %v", afterSecond, err)
	}
	if len(afterSecond.ApprovedBy) != 2 {
		t.Fatalf("approved by = %v", afterSecond.ApprovedBy)
	}
	want := []string{"approval.created", "approval.partial", "approval.approved"}
	if len(events.events) != len(want) {
		t.Fatalf("events = %+v", events.events)
	}
	for i, event := range events.events {
		if event.Kind != want[i] || event.ID != request.ID || event.TenantID != scope().TenantID || len(event.Attributes) != 1 {
			t.Fatalf("event = %+v", event)
		}
	}
}

func TestSingleApprovalLifecycle(t *testing.T) {
	service := NewService(newMemStore(), func() time.Time { return time.Unix(1, 0) })
	ctx := context.Background()
	request, err := service.Create(ctx, scope(), Request{ID: "a3", Requester: "alice", Action: "delegation.grant", Target: "agent-b"})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.Approve(ctx, scope(), request.ID, "bob")
	if err != nil || approved.Status != StatusApproved {
		t.Fatalf("approve = %+v, %v", approved, err)
	}
	// Deciding again is rejected (not pending).
	if _, err := service.Approve(ctx, scope(), request.ID, "dave"); !errors.Is(err, ErrNotPending) {
		t.Fatalf("decide after approval error = %v", err)
	}
	// A separate request can be rejected.
	rejected, err := service.Create(ctx, scope(), Request{ID: "a4", Requester: "alice", Action: "external_agent.suspend", Target: "rel-1"})
	if err != nil {
		t.Fatal(err)
	}
	afterReject, err := service.Reject(ctx, scope(), rejected.ID, "bob")
	if err != nil || afterReject.Status != StatusRejected {
		t.Fatalf("reject = %+v, %v", afterReject, err)
	}
}

// S2 regression: with an identity verifier installed, a single admin cannot
// forge a second approver to satisfy dual approval, and forged actors are
// rejected on both approve and reject.
func TestApproverVerifierRejectsForgedActor(t *testing.T) {
	active := map[string]bool{"bob": true}
	service := NewService(newMemStore(), func() time.Time { return time.Unix(1, 0) })
	service.SetApproverVerifier(func(_ context.Context, _ tenancy.TenantScope, approver string) error {
		if active[approver] {
			return nil
		}
		return ErrUnknownApprover
	})
	ctx := context.Background()
	request, err := service.Create(ctx, scope(), Request{ID: "a5", Requester: "alice", Action: "external_agent.suspend", Target: "rel-1", DualApproval: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(ctx, scope(), request.ID, "carol"); !errors.Is(err, ErrUnknownApprover) {
		t.Fatalf("forged first approver error = %v", err)
	}
	first, err := service.Approve(ctx, scope(), request.ID, "bob")
	if err != nil || first.Status != StatusPending {
		t.Fatalf("first approve = %+v, %v", first, err)
	}
	if _, err := service.Approve(ctx, scope(), request.ID, "forged-second"); !errors.Is(err, ErrUnknownApprover) {
		t.Fatalf("forged second approver error = %v", err)
	}
	current, err := service.store.Get(ctx, scope(), request.ID)
	if err != nil || current.Status != StatusPending {
		t.Fatalf("request after forged decision = %+v, %v", current, err)
	}
	rejectTarget, err := service.Create(ctx, scope(), Request{ID: "a6", Requester: "alice", Action: "external_agent.suspend", Target: "rel-2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Reject(ctx, scope(), rejectTarget.ID, "mallory"); !errors.Is(err, ErrUnknownApprover) {
		t.Fatalf("forged reviewer error = %v", err)
	}
}

// Without a verifier the legacy behavior is unchanged (no identity check).
func TestApproverVerifierOptional(t *testing.T) {
	service := NewService(newMemStore(), func() time.Time { return time.Unix(1, 0) })
	ctx := context.Background()
	request, err := service.Create(ctx, scope(), Request{ID: "a7", Requester: "alice", Action: "delegation.grant", Target: "agent-b"})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.Approve(ctx, scope(), request.ID, "anyone")
	if err != nil || approved.Status != StatusApproved {
		t.Fatalf("approve without verifier = %+v, %v", approved, err)
	}
}
