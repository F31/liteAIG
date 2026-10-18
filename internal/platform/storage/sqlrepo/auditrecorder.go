package sqlrepo

import (
	"context"
	"fmt"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
)

// AuditRecorder writes cross-domain lifecycle actions (account administration,
// credential and key changes, authentication events) to audit_events.
type AuditRecorder struct {
	db        DBTX
	ids       contracts.IDGenerator
	uuidActor bool
}

// NewAuditRecorder builds a recorder. uuidActor should be true for Postgres,
// whose actor_id column is UUID-typed; service actors such as "anonymous" are
// then mapped to the zero UUID. SQLite keeps the human-readable actor.
func NewAuditRecorder(db DBTX, ids contracts.IDGenerator, uuidActor ...bool) *AuditRecorder {
	enabled := false
	if len(uuidActor) > 0 {
		enabled = uuidActor[0]
	}
	return &AuditRecorder{db: db, ids: ids, uuidActor: enabled}
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
		id, tenantID, r.actorID(actorID), action, resourceType, resourceID, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("write audit event: %w", err)
	}
	return nil
}

// RecordSystem appends one system-scoped audit event. It is used for global
// mutations that intentionally do not carry a tenant scope.
// auditActorID maps human-readable service actors (anonymous/bootstrap) onto
// the UUID column without losing the audit row. Real admin IDs are already
// UUIDs and pass through unchanged.
func (r *AuditRecorder) actorID(actor string) string {
	if !r.uuidActor {
		return actor
	}
	switch actor {
	case "", "anonymous", "bootstrap", "system":
		return "00000000-0000-0000-0000-000000000000"
	default:
		return actor
	}
}

func (r *AuditRecorder) RecordSystem(ctx context.Context, actorID, action, resourceType, resourceID string) error {
	id, err := r.ids.New()
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO audit_events(id, tenant_id, scope, actor_id, action, resource_type, resource_id, result, details, occurred_at)
VALUES ($1, NULL, 'system', $2, $3, $4, $5, 'success', '{}', $6)`,
		id, r.actorID(actorID), action, resourceType, resourceID, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("write system audit event: %w", err)
	}
	return nil
}
