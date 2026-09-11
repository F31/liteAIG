package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
)

type fakeOutboxStore struct {
	due       []sqlrepo.OutboxEvent
	delivered []string
	failed    []string
	exhausted []bool
	err       error
}

func (s *fakeOutboxStore) Due(context.Context, int, time.Time) ([]sqlrepo.OutboxEvent, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.due, nil
}
func (s *fakeOutboxStore) MarkDelivered(_ context.Context, id string) error {
	s.delivered = append(s.delivered, id)
	return nil
}
func (s *fakeOutboxStore) MarkFailed(_ context.Context, id string, _ time.Time, exhausted bool) error {
	s.failed = append(s.failed, id)
	s.exhausted = append(s.exhausted, exhausted)
	return nil
}

func TestOutboxDeliveryWorkerDeliversAndMarks(t *testing.T) {
	now := time.Unix(100, 0)
	store := &fakeOutboxStore{due: []sqlrepo.OutboxEvent{
		{ID: "event-a", Kind: "system_config.update", Attributes: map[string]string{"severity": "medium"}, OccurredAt: now},
	}}
	sink := &recordingEventSink{}
	worker := newOutboxDeliveryWorker(store, sink, fixedClock{now: now})
	if err := worker.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 1 || sink.events[0].Kind != "system_config.update" {
		t.Fatalf("delivered = %+v", sink.events)
	}
	if len(store.delivered) != 1 || store.delivered[0] != "event-a" {
		t.Fatalf("delivered ids = %+v", store.delivered)
	}
}

func TestOutboxDeliveryWorkerRetriesUntilExhausted(t *testing.T) {
	now := time.Unix(100, 0)
	store := &fakeOutboxStore{due: []sqlrepo.OutboxEvent{{ID: "event-b", Kind: "file.access", OccurredAt: now, Attempts: domainEventOutboxMaxAttempts - 1}}}
	worker := newOutboxDeliveryWorker(store, failingEventSink{}, fixedClock{now: now})
	if err := worker.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.failed) != 1 || store.failed[0] != "event-b" || !store.exhausted[0] {
		t.Fatalf("failed = %v exhausted = %v", store.failed, store.exhausted)
	}
}

func TestOutboxDeliveryWorkerSchedulesBackoff(t *testing.T) {
	if backoffFor(1) != time.Second || backoffFor(2) != 4*time.Second || backoffFor(20) != domainEventOutboxBackoffCap {
		t.Fatalf("backoffFor = %v %v %v", backoffFor(1), backoffFor(2), backoffFor(20))
	}
}

type failingEventSink struct{}

func (failingEventSink) Emit(context.Context, contracts.DomainEvent) error { return errors.New("down") }
