package sqlrepo

import (
	"context"
	"database/sql"
	"time"

	"github.com/F31/liteAIG/internal/tenancy"
)

// ToolCallEvent is one persisted tool invocation record.
type ToolCallEvent struct {
	ID, TenantID, ProjectID, RequestID, SessionID, TaskID, AgentID, UserID, ToolID, ToolName string
	OccurredAt                                                                               time.Time
}

// ToolCallStore persists tenant-scoped tool call events.
type ToolCallStore struct{ db *sql.DB }

func NewToolCallStore(db *sql.DB) *ToolCallStore { return &ToolCallStore{db: db} }

func (s *ToolCallStore) Create(ctx context.Context, scope tenancy.TenantScope, event ToolCallEvent) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if event.TenantID != scope.TenantID {
		return tenancy.ErrScopeMismatch
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO tool_call_events(id,tenant_id,project_id,request_id,session_id,task_id,agent_id,user_id,tool_id,tool_name,occurred_at) VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),$9,$10,$11)`,
		event.ID, event.TenantID, event.ProjectID, event.RequestID, event.SessionID, event.TaskID, event.AgentID, event.UserID, event.ToolID, event.ToolName, event.OccurredAt)
	return err
}

func (s *ToolCallStore) List(ctx context.Context, scope tenancy.TenantScope, limit int) ([]ToolCallEvent, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,tenant_id,COALESCE(project_id,''),COALESCE(request_id,''),COALESCE(session_id,''),COALESCE(task_id,''),COALESCE(agent_id,''),COALESCE(user_id,''),tool_id,tool_name,occurred_at FROM tool_call_events WHERE tenant_id=$1 ORDER BY occurred_at DESC LIMIT $2`, scope.TenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ToolCallEvent
	for rows.Next() {
		var event ToolCallEvent
		var occurredAt databaseTime
		if err := rows.Scan(&event.ID, &event.TenantID, &event.ProjectID, &event.RequestID, &event.SessionID, &event.TaskID, &event.AgentID, &event.UserID, &event.ToolID, &event.ToolName, &occurredAt); err != nil {
			return nil, err
		}
		event.OccurredAt = occurredAt.Time
		result = append(result, event)
	}
	return result, rows.Err()
}
