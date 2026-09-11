package sqlrepo

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/observability/alert"
	"github.com/F31/liteAIG/internal/tenancy"
)

// AlertAuditRecorder writes alert lifecycle actions to audit_events.
type AlertAuditRecorder struct {
	db  *sql.DB
	ids contracts.IDGenerator
}

func NewAlertAuditRecorder(db *sql.DB, ids contracts.IDGenerator) *AlertAuditRecorder {
	return &AlertAuditRecorder{db: db, ids: ids}
}

func (r *AlertAuditRecorder) RecordAction(ctx context.Context, scope tenancy.TenantScope, actorID, action, resourceID string) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	id, err := r.ids.New()
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO audit_events(id, tenant_id, scope, actor_id, action, resource_type, resource_id, result, details, occurred_at)
VALUES ($1, $2, 'tenant', $3, $4, 'alert', $5, 'success', '{}', $6)`,
		id, scope.TenantID, actorID, action, resourceID, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("write alert audit: %w", err)
	}
	return nil
}

var _ alert.Auditor = (*AlertAuditRecorder)(nil)
