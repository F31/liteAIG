package sqlrepo

import (
	"context"
	"database/sql"

	"github.com/F31/liteAIG/internal/observability/audit"
)

// AuditRepository reads the audit_events table.
type AuditRepository struct{ db *sql.DB }

func NewAuditRepository(db *sql.DB) *AuditRepository { return &AuditRepository{db: db} }

const defaultAuditLimit = 200

func (r *AuditRepository) List(ctx context.Context, tenantID string, limit, offset int) ([]audit.Record, error) {
	if limit <= 0 {
		limit = defaultAuditLimit
	}
	if limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT a.id, a.action, a.resource_type, COALESCE(a.resource_id,''), COALESCE(a.result,''),
       COALESCE(lu.username, a.actor_id), a.occurred_at
FROM audit_events a
LEFT JOIN local_admins lu ON lu.id = a.actor_id
WHERE a.tenant_id = $1
ORDER BY a.occurred_at DESC, a.id DESC
LIMIT $2 OFFSET $3`, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []audit.Record
	for rows.Next() {
		var record audit.Record
		var occurredAt databaseTime
		if err := rows.Scan(&record.ID, &record.Action, &record.ResourceType, &record.ResourceID, &record.Result, &record.Actor, &occurredAt); err != nil {
			return nil, err
		}
		if !occurredAt.Time.IsZero() {
			record.OccurredAt = occurredAt.Time.UTC()
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

var _ audit.Repository = (*AuditRepository)(nil)
