package budget

import (
	"context"
	"time"
)

// AuditEvent is a reservation lifecycle fact used for audit and recovery.
type AuditEvent struct {
	ID            string
	TenantID      string
	ReservationID string
	ScopeType     string
	ScopeID       string
	Estimate      float64
	Actual        *float64
	Status        string
	WindowKeys    []string
	CreatedAt     time.Time
	FinalizedAt   *time.Time
}

// AuditRepository persists reservation lifecycle facts asynchronously.
type AuditRepository interface {
	Record(context.Context, AuditEvent) error
}
