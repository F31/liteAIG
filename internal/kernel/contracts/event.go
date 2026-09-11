package contracts

import (
	"context"
	"time"
)

// DomainEvent is a redacted fact emitted by a business module.
type DomainEvent struct {
	ID         string
	Kind       string
	OccurredAt time.Time
	TenantID   string
	ProjectID  string
	RequestID  string
	Attributes map[string]string
}

// EventSink receives domain events without exposing an observability implementation.
type EventSink interface {
	Emit(context.Context, DomainEvent) error
}
