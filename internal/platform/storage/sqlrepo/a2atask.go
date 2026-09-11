package sqlrepo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/F31/liteAIG/internal/tenancy"
)

// A2A task lifecycle statuses persisted on a2a_tasks.status.
const (
	A2ATaskPending   = "pending"
	A2ATaskRunning   = "running"
	A2ATaskCompleted = "completed"
	A2ATaskFailed    = "failed"
	A2ATaskCancelled = "cancelled"
)

// A2ATask is one durable outbound-relay task row. Message holds a small
// snapshot of the outbound text this gateway sent (never secrets) so a retried
// request can be correlated with what actually left the process; Result holds
// the completion text reused by idempotent replays so a deduplicated request
// returns an identical body.
type A2ATask struct {
	TaskID          string
	TenantID        string
	ExternalAgentID string
	ProjectID       string
	RequestID       string
	IdempotencyKey  string
	Status          string
	Message         string
	Result          string
	Hops            int
	Calls           int
	Attempts        int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// A2ATaskStore persists tenant-scoped durable A2A task state.
type A2ATaskStore struct{ db *sql.DB }

func NewA2ATaskStore(db *sql.DB) *A2ATaskStore { return &A2ATaskStore{db: db} }

// Create records a pending task. A task already holding the idempotency key
// (within the tenant) wins and the insert is a no-op, so concurrent identical
// sends never create a second durable task; callers resolve ownership with
// GetByTask and fall back to GetByIdempotency on a conflict.
func (s *A2ATaskStore) Create(ctx context.Context, scope tenancy.TenantScope, task A2ATask) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if task.TenantID != scope.TenantID {
		return tenancy.ErrScopeMismatch
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO a2a_tasks(
    task_id, tenant_id, external_agent_id, project_id, request_id, idempotency_key,
    status, message, hop_count, call_count, attempts
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`,
		task.TaskID, task.TenantID, task.ExternalAgentID, task.ProjectID, task.RequestID,
		task.IdempotencyKey, task.Status, task.Message, task.Hops, task.Calls, task.Attempts)
	return err
}

// GetByTask returns the durable task for task_id within the tenant scope.
func (s *A2ATaskStore) GetByTask(ctx context.Context, scope tenancy.TenantScope, taskID string) (A2ATask, bool, error) {
	if err := scope.Validate(); err != nil {
		return A2ATask{}, false, err
	}
	return s.get(ctx, `WHERE tenant_id=$1 AND task_id=$2`, scope.TenantID, taskID)
}

// GetByIdempotency returns the durable task holding the idempotency key within
// the tenant scope.
func (s *A2ATaskStore) GetByIdempotency(ctx context.Context, scope tenancy.TenantScope, key string) (A2ATask, bool, error) {
	if err := scope.Validate(); err != nil {
		return A2ATask{}, false, err
	}
	return s.get(ctx, `WHERE tenant_id=$1 AND idempotency_key=$2`, scope.TenantID, key)
}

func (s *A2ATaskStore) get(ctx context.Context, predicate string, args ...any) (A2ATask, bool, error) {
	var task A2ATask
	var createdAt, updatedAt databaseTime
	err := s.db.QueryRowContext(ctx, `
SELECT task_id, tenant_id, COALESCE(external_agent_id,''), COALESCE(project_id,''),
       COALESCE(request_id,''), idempotency_key, status, message, result,
       hop_count, call_count, attempts, created_at, updated_at
FROM a2a_tasks `+predicate, args...).Scan(
		&task.TaskID, &task.TenantID, &task.ExternalAgentID, &task.ProjectID,
		&task.RequestID, &task.IdempotencyKey, &task.Status, &task.Message, &task.Result,
		&task.Hops, &task.Calls, &task.Attempts, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return A2ATask{}, false, nil
	}
	if err != nil {
		return A2ATask{}, false, err
	}
	task.CreatedAt, task.UpdatedAt = createdAt.Time, updatedAt.Time
	return task, true, nil
}

// AdvanceCounters atomically consumes hops/calls and records one more attempt,
// refusing the admission when it would push the durable task past maxHops or
// maxCalls. It reports whether the counter advance was applied.
func (s *A2ATaskStore) AdvanceCounters(ctx context.Context, scope tenancy.TenantScope, taskID string, hops, calls, maxHops, maxCalls int) (bool, error) {
	if err := scope.Validate(); err != nil {
		return false, err
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE a2a_tasks
SET hop_count = hop_count + $3, call_count = call_count + $4, attempts = attempts + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = $1 AND task_id = $2
  AND hop_count + $3 <= $5 AND call_count + $4 <= $6`,
		scope.TenantID, taskID, hops, calls, maxHops, maxCalls)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

// TryStart atomically claims a task for execution. Only pending/failed/
// cancelled tasks may transition to running; stale running rows are recovered by
// ReapStaleRunning before callers attempt this CAS.
func (s *A2ATaskStore) TryStart(ctx context.Context, scope tenancy.TenantScope, taskID string, staleBefore time.Time) (bool, error) {
	if err := scope.Validate(); err != nil {
		return false, err
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE a2a_tasks
SET status = 'running', result = '', updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = $1 AND task_id = $2
  AND status IN ('pending','failed','cancelled')`, scope.TenantID, taskID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

// ReapStaleRunning marks abandoned running tasks failed so a later request can
// claim them through TryStart. It is intentionally tenant-scoped.
func (s *A2ATaskStore) ReapStaleRunning(ctx context.Context, scope tenancy.TenantScope, staleBefore time.Time) (int64, error) {
	if err := scope.Validate(); err != nil {
		return 0, err
	}
	cutoff := staleBefore.UTC().Format("2006-01-02 15:04:05")
	result, err := s.db.ExecContext(ctx, `
UPDATE a2a_tasks
SET status = 'failed', result = '', updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = $1 AND status = 'running' AND updated_at < $2`, scope.TenantID, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// SetStatusResult transitions the durable task to status and stores result
// (empty for every status except completed).
func (s *A2ATaskStore) SetStatusResult(ctx context.Context, scope tenancy.TenantScope, taskID, status, result string) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE a2a_tasks SET status = $3, result = $4, updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = $1 AND task_id = $2`, scope.TenantID, taskID, status, result)
	return err
}
