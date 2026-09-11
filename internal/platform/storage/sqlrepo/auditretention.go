package sqlrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/F31/liteAIG/internal/observability/audit"
)

// AuditRetentionRepository persists audit retention policies and purges
// expired audit events. Retention is opt-in: tenants without a policy keep
// their trail forever, and the purge loop only considers tenants that opted
// into a bounded window.
type AuditRetentionRepository struct{ db *sql.DB }

func NewAuditRetentionRepository(db *sql.DB) *AuditRetentionRepository {
	return &AuditRetentionRepository{db: db}
}

func (r *AuditRetentionRepository) Get(ctx context.Context, tenantID string) (audit.RetentionPolicy, error) {
	var policy audit.RetentionPolicy
	var updatedBy sql.NullString
	var updatedAt databaseTime
	err := r.db.QueryRowContext(ctx, `
SELECT tenant_id, retention_days, updated_by, updated_at
FROM audit_retention
WHERE tenant_id=$1`, tenantID).Scan(&policy.TenantID, &policy.RetentionDays, &updatedBy, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return audit.RetentionPolicy{}, nil
	}
	if err != nil {
		return audit.RetentionPolicy{}, fmt.Errorf("get audit retention: %w", err)
	}
	policy.UpdatedBy = updatedBy.String
	policy.UpdatedAt = updatedAt.Time
	return policy, nil
}

func (r *AuditRetentionRepository) Set(ctx context.Context, tenantID string, retentionDays int, actorID string) error {
	if tenantID == "" {
		return errors.New("tenant id is required")
	}
	updatedBy := sql.NullString{String: actorID, Valid: actorID != ""}
	now := time.Now().UTC()
	var err error
	if retentionDays <= 0 {
		// Keep forever: removing the policy is the explicit opt-out.
		_, err = r.db.ExecContext(ctx, `DELETE FROM audit_retention WHERE tenant_id=$1`, tenantID)
	} else {
		_, err = r.db.ExecContext(ctx, `
INSERT INTO audit_retention(tenant_id, retention_days, updated_by, updated_at)
VALUES ($1,$2,$3,$4)
ON CONFLICT(tenant_id) DO UPDATE SET
    retention_days=excluded.retention_days,
    updated_by=excluded.updated_by,
    updated_at=excluded.updated_at`, tenantID, retentionDays, updatedBy, now)
	}
	if err != nil {
		return fmt.Errorf("set audit retention: %w", err)
	}
	return nil
}

func (r *AuditRetentionRepository) List(ctx context.Context) ([]audit.RetentionPolicy, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT tenant_id, retention_days, updated_by, updated_at
FROM audit_retention`)
	if err != nil {
		return nil, fmt.Errorf("list audit retention: %w", err)
	}
	defer rows.Close()
	var result []audit.RetentionPolicy
	for rows.Next() {
		var policy audit.RetentionPolicy
		var updatedBy sql.NullString
		var updatedAt databaseTime
		if err := rows.Scan(&policy.TenantID, &policy.RetentionDays, &updatedBy, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan audit retention: %w", err)
		}
		policy.UpdatedBy = updatedBy.String
		policy.UpdatedAt = updatedAt.Time
		result = append(result, policy)
	}
	return result, rows.Err()
}

func (r *AuditRetentionRepository) Purge(ctx context.Context, tenantID string, before time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `
DELETE FROM audit_events
WHERE tenant_id=$1 AND occurred_at < $2`, tenantID, before.UTC())
	if err != nil {
		return 0, fmt.Errorf("purge audit events: %w", err)
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("purge audit events count: %w", err)
	}
	return removed, nil
}

var _ audit.RetentionStore = (*AuditRetentionRepository)(nil)
