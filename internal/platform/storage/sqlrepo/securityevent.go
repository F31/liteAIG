package sqlrepo

import (
	"context"
	"database/sql"
	"errors"

	"github.com/F31/liteAIG/internal/guardrail"
	"github.com/F31/liteAIG/internal/tenancy"
)

type SecurityEventStore struct{ db *sql.DB }

func NewSecurityEventStore(db *sql.DB) *SecurityEventStore { return &SecurityEventStore{db: db} }

func (s *SecurityEventStore) Create(ctx context.Context, scope tenancy.TenantScope, event guardrail.SecurityEvent) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if event.TenantID != scope.TenantID {
		return tenancy.ErrScopeMismatch
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO security_events(id,tenant_id,project_id,request_id,policy_id,rule_id,action,content_hash,snapshot_version,occurred_at) VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''),$5,$6,$7,$8,$9,$10)`, event.ID, event.TenantID, event.ProjectID, event.RequestID, event.PolicyID, event.RuleID, event.Action, event.ContentHash, event.SnapshotVersion, event.OccurredAt)
	return err
}

func (s *SecurityEventStore) List(ctx context.Context, scope tenancy.TenantScope, limit int) ([]guardrail.SecurityEvent, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, errors.New("security event limit must be positive")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,tenant_id,COALESCE(project_id,''),COALESCE(request_id,''),policy_id,rule_id,action,content_hash,snapshot_version,occurred_at FROM security_events WHERE tenant_id=$1 ORDER BY occurred_at DESC LIMIT $2`, scope.TenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []guardrail.SecurityEvent
	for rows.Next() {
		var event guardrail.SecurityEvent
		var occurredAt databaseTime
		if err := rows.Scan(&event.ID, &event.TenantID, &event.ProjectID, &event.RequestID, &event.PolicyID, &event.RuleID, &event.Action, &event.ContentHash, &event.SnapshotVersion, &occurredAt); err != nil {
			return nil, err
		}
		event.OccurredAt = occurredAt.Time
		result = append(result, event)
	}
	return result, rows.Err()
}

var _ guardrail.SecurityEventStore = (*SecurityEventStore)(nil)
