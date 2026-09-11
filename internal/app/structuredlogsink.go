package app

import (
	"context"

	"github.com/F31/liteAIG/internal/kernel/contracts"
)

// logThenEventSink emits each domain event to a best-effort structured log
// sink (OTLP) before forwarding to the durable/notification chain. Log-sink
// failures never fail the caller; the event still reaches the durable outbox.
type logThenEventSink struct {
	log  contracts.EventSink
	next contracts.EventSink
}

func (s logThenEventSink) Emit(ctx context.Context, event contracts.DomainEvent) error {
	if s.log != nil {
		_ = s.log.Emit(ctx, event)
	}
	if s.next == nil {
		return nil
	}
	return s.next.Emit(ctx, event)
}
