// Package audit owns the cross-domain audit event trail (audit_events table).
package audit

import (
	"context"
	"time"
)

// Record is one audit trail entry.
type Record struct {
	ID           string `json:"id"`
	Action       string `json:"action"`
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceId"`
	Result       string `json:"result"`
	// Actor is the acting local admin's username, or "anonymous" for
	// unauthenticated attempts.
	Actor string `json:"actor"`
	// OccurredAt is when the audited event happened.
	OccurredAt time.Time `json:"occurredAt"`
}

// Repository reads the tenant-scoped audit trail, newest first. limit bounds
// the page (0 picks a sane default); offset skips already-seen rows.
type Repository interface {
	List(context.Context, string, int, int) ([]Record, error)
}

// RetentionPolicy is the tenant-scoped audit retention setting. A value of
// zero RetentionDays means "keep forever" (no explicit policy).
type RetentionPolicy struct {
	TenantID      string
	RetentionDays int
	UpdatedBy     string
	UpdatedAt     time.Time
}

// RetentionStore persists audit retention policies and purges expired audit
// events. The purge loop enumerates policies with List and issues per-tenant
// cuts, keeping the SQL portable across SQLite and Postgres.
type RetentionStore interface {
	Get(context.Context, string) (RetentionPolicy, error)
	// Set upserts a policy; RetentionDays <= 0 removes the row (keep forever).
	Set(context.Context, string, int, string) error
	List(context.Context) ([]RetentionPolicy, error)
	// Purge deletes audit_events older than before for one tenant and returns
	// the number of rows removed.
	Purge(context.Context, string, time.Time) (int64, error)
}
