package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
)

func TestDurableEventSinkPersistsAndMarksDelivered(t *testing.T) {
	outbox := &recordingOutbox{}
	next := &recordingEventSink{}
	sink := newDurableEventSink(outbox, next)
	event := contracts.DomainEvent{ID: "event", Kind: "file.access", OccurredAt: time.Unix(1, 0)}
	if err := sink.Emit(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(outbox.events) != 1 || outbox.events[0].ID != "event" {
		t.Fatalf("outbox events = %+v", outbox.events)
	}
	if len(next.events) != 1 {
		t.Fatalf("forwarded events = %+v", next.events)
	}
	if len(outbox.delivered) != 1 || outbox.delivered[0] != "event" {
		t.Fatalf("delivered ids = %+v", outbox.delivered)
	}
}

func TestDurableEventSinkFallsBackWhenOutboxMissing(t *testing.T) {
	next := &recordingEventSink{}
	sink := newDurableEventSink(nil, next)
	if err := sink.Emit(context.Background(), contracts.DomainEvent{ID: "event", Kind: "file.access"}); err != nil {
		t.Fatal(err)
	}
	if len(next.events) != 1 {
		t.Fatalf("forwarded events = %+v", next.events)
	}
}

func TestDurableEventSinkDoesNotFailRequestOnOutboxError(t *testing.T) {
	sink := newDurableEventSink(failingOutbox{}, &recordingEventSink{})
	if err := sink.Emit(context.Background(), contracts.DomainEvent{ID: "event", Kind: "file.access"}); err != nil {
		t.Fatal(err)
	}
}

type recordingOutbox struct {
	events    []contracts.DomainEvent
	delivered []string
}

func (s *recordingOutbox) Enqueue(_ context.Context, event contracts.DomainEvent) error {
	s.events = append(s.events, event)
	return nil
}

func (s *recordingOutbox) MarkDelivered(_ context.Context, id string) error {
	s.delivered = append(s.delivered, id)
	return nil
}

type failingOutbox struct{}

func (failingOutbox) Enqueue(context.Context, contracts.DomainEvent) error { return errors.New("down") }
func (failingOutbox) MarkDelivered(context.Context, string) error          { return errors.New("down") }

type recordingEventSink struct{ events []contracts.DomainEvent }

func (s *recordingEventSink) Emit(_ context.Context, event contracts.DomainEvent) error {
	s.events = append(s.events, event)
	return nil
}

func TestLogThenEventSinkForwardsAndSwallowsLogFailures(t *testing.T) {
	logSink := &recordingEventSink{}
	next := &recordingEventSink{}
	sink := logThenEventSink{log: logSink, next: next}
	event := contracts.DomainEvent{ID: "event", Kind: "guardrail.match", OccurredAt: time.Unix(1, 0)}
	if err := sink.Emit(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(logSink.events) != 1 || len(next.events) != 1 {
		t.Fatalf("log=%d next=%d", len(logSink.events), len(next.events))
	}

	failing := logThenEventSink{log: failingLogSink{}, next: next}
	if err := failing.Emit(context.Background(), event); err != nil {
		t.Fatalf("log failure must not fail the caller: %v", err)
	}
	if len(next.events) != 2 {
		t.Fatalf("next events = %d", len(next.events))
	}

	onlyLog := logThenEventSink{log: logSink}
	if err := onlyLog.Emit(context.Background(), event); err != nil {
		t.Fatal(err)
	}
}

type failingLogSink struct{}

func (failingLogSink) Emit(context.Context, contracts.DomainEvent) error {
	return errors.New("log down")
}
