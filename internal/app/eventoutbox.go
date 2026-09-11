package app

import (
	"context"
	"log"

	"github.com/F31/liteAIG/internal/kernel/contracts"
)

type domainEventOutbox interface {
	Enqueue(context.Context, contracts.DomainEvent) error
	MarkDelivered(context.Context, string) error
}

// durableEventSink forwards events immediately like the previous in-process
// chain, and additionally persists them to the outbox first. A successful
// forward marks the outbox row delivered; a crash in between leaves a queued
// row that the outbox delivery worker recovers after the claim grace window,
// giving notifications and analytics at-least-once semantics.
type durableEventSink struct {
	outbox domainEventOutbox
	next   contracts.EventSink
}

func newDurableEventSink(outbox domainEventOutbox, next contracts.EventSink) contracts.EventSink {
	if outbox == nil {
		return next
	}
	return durableEventSink{outbox: outbox, next: next}
}

func (s durableEventSink) Emit(ctx context.Context, event contracts.DomainEvent) error {
	if err := s.outbox.Enqueue(ctx, event); err != nil {
		log.Print("lite: domain event outbox enqueue failed")
	}
	if s.next == nil {
		return nil
	}
	err := s.next.Emit(ctx, event)
	if err == nil {
		s.outbox.MarkDelivered(ctx, event.ID)
	}
	return err
}
