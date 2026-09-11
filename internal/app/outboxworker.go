package app

import (
	"context"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
)

// domainEventOutboxStore is the repository surface the outbox delivery worker
// depends on. It matches sqlrepo.DomainEventOutbox.
type domainEventOutboxStore interface {
	Due(context.Context, int, time.Time) ([]sqlrepo.OutboxEvent, error)
	MarkDelivered(context.Context, string) error
	MarkFailed(context.Context, string, time.Time, bool) error
}

const (
	domainEventOutboxMaxAttempts = 5
	domainEventOutboxBackoffCap  = 5 * time.Minute
)

// outboxDeliveryWorker drains queued domain events to the notification and
// analytics sink. Delivery is best-effort for the sink but durable in the
// outbox: a crash between enqueue and delivery is retried on the next run.
type outboxDeliveryWorker struct {
	store domainEventOutboxStore
	sink  contracts.EventSink
	clock contracts.Clock
}

func newOutboxDeliveryWorker(store domainEventOutboxStore, sink contracts.EventSink, clock contracts.Clock) *outboxDeliveryWorker {
	return &outboxDeliveryWorker{store: store, sink: sink, clock: clock}
}

func (w *outboxDeliveryWorker) Drain(ctx context.Context) error {
	if w.store == nil || w.sink == nil {
		return nil
	}
	now := w.clock.Now()
	events, err := w.store.Due(ctx, 64, now)
	if err != nil {
		return err
	}
	for _, outbox := range events {
		event := contracts.DomainEvent{
			ID:         outbox.ID,
			Kind:       outbox.Kind,
			OccurredAt: outbox.OccurredAt,
			TenantID:   outbox.TenantID,
			ProjectID:  outbox.ProjectID,
			RequestID:  outbox.RequestID,
			Attributes: outbox.Attributes,
		}
		if err := w.sink.Emit(ctx, event); err != nil {
			attempt := outbox.Attempts + 1
			exhausted := attempt >= domainEventOutboxMaxAttempts
			_ = w.store.MarkFailed(ctx, outbox.ID, now.Add(backoffFor(attempt)), exhausted)
			continue
		}
		_ = w.store.MarkDelivered(ctx, outbox.ID)
	}
	return nil
}

func backoffFor(attempt int) time.Duration {
	duration := time.Duration(attempt*attempt) * time.Second
	if duration > domainEventOutboxBackoffCap {
		return domainEventOutboxBackoffCap
	}
	return duration
}
