package sqlrepo

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
)

// AuditRecorder writes cross-domain lifecycle actions (account administration,
// credential and key changes, authentication events) to audit_events.
type AuditRecorder struct {
	db  *sql.DB
	ids contracts.IDGenerator
}

func NewAuditRecorder(db *sql.DB, ids contracts.IDGenerator) *AuditRecorder {
	return &AuditRecorder{db: db, ids: ids}
}

// Record appends one tenant-scoped audit event. actorID is the acting local
// admin (or "anonymous" for unauthenticated attempts); resourceType labels the
// affected object class and resourceID the object.
func (r *AuditRecorder) Record(ctx context.Context, tenantID, actorID, action, resourceType, resourceID string) error {
	id, err := r.ids.New()
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO audit_events(id, tenant_id, scope, actor_id, action, resource_type, resource_id, result, details, occurred_at)
VALUES ($1, $2, 'tenant', $3, $4, $5, $6, 'success', '{}', $7)`,
		id, tenantID, actorID, action, resourceType, resourceID, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("write audit event: %w", err)
	}
	return nil
}

// RecordSystem appends one system-scoped audit event. It is used for global
// mutations that intentionally do not carry a tenant scope.
func (r *AuditRecorder) RecordSystem(ctx context.Context, actorID, action, resourceType, resourceID string) error {
	id, err := r.ids.New()
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO audit_events(id, tenant_id, scope, actor_id, action, resource_type, resource_id, result, details, occurred_at)
VALUES ($1, NULL, 'system', $2, $3, $4, $5, 'success', '{}', $6)`,
		id, actorID, action, resourceType, resourceID, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("write system audit event: %w", err)
	}
	return nil
}
