package guardrail

import (
	"context"
	"time"

	"github.com/F31/liteAIG/internal/tenancy"
)

type SecurityEvent struct {
	ID, TenantID, ProjectID, RequestID, PolicyID, RuleID, Action, ContentHash string
	SnapshotVersion                                                           int64
	OccurredAt                                                                time.Time
}

type SecurityEventStore interface {
	Create(context.Context, tenancy.TenantScope, SecurityEvent) error
	List(context.Context, tenancy.TenantScope, int) ([]SecurityEvent, error)
}
