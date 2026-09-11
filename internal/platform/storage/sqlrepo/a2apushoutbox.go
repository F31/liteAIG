package sqlrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/F31/liteAIG/internal/federation"
)

// A2APushOutboxStore persists signed A2A push callbacks until delivery or a
// bounded retry budget is exhausted.
type A2APushOutboxStore struct{ db *sql.DB }

const a2aPushClaimTimeout = 5 * time.Minute

type A2APushOutboxSummary struct {
	Pending               int
	Sending               int
	Delivered             int
	Failed                int
	EarliestNextAttemptAt *time.Time
}

// A2APushOutboxFailure is one sanitized outbox row for operator drill-down. It
// deliberately excludes the callback URL, bearer token, and payload body.
type A2APushOutboxFailure struct {
	ID            string
	TaskID        string
	Status        string
	Attempts      int
	MaxAttempts   int
	LastError     string
	NextAttemptAt time.Time
	UpdatedAt     time.Time
}

func NewA2APushOutboxStore(db *sql.DB) *A2APushOutboxStore {
	return &A2APushOutboxStore{db: db}
}

func (s *A2APushOutboxStore) EnqueueForTask(ctx context.Context, delivery federation.A2APushDelivery) error {
	if delivery.ID == "" || delivery.TaskID == "" || delivery.CallbackURL == "" || len(delivery.Payload) == 0 {
		return errors.New("a2a push delivery is incomplete")
	}
	maxAttempts := delivery.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	result, err := s.db.ExecContext(ctx, `
INSERT INTO a2a_push_outbox(id, tenant_id, task_id, callback_url, bearer_token, payload, max_attempts)
SELECT $1, tenant_id, task_id, $3, $4, $5, $6
FROM a2a_tasks
WHERE task_id = $2
ON CONFLICT DO NOTHING`, delivery.ID, delivery.TaskID, delivery.CallbackURL, delivery.BearerToken, string(delivery.Payload), maxAttempts)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return errors.New("a2a task not found for push delivery")
	}
	return nil
}

func (s *A2APushOutboxStore) Due(ctx context.Context, limit int, now time.Time) ([]federation.A2APushDelivery, error) {
	if limit <= 0 {
		limit = 25
	}
	nowText := now.UTC().Format("2006-01-02 15:04:05")
	staleClaimBefore := now.Add(-a2aPushClaimTimeout).UTC().Format("2006-01-02 15:04:05")
	rows, err := s.db.QueryContext(ctx, `
UPDATE a2a_push_outbox
SET status = 'sending', updated_at = CURRENT_TIMESTAMP
WHERE id IN (
	SELECT id
	FROM a2a_push_outbox
	WHERE attempts < max_attempts
	  AND ((status = 'pending' AND next_attempt_at <= $1) OR (status = 'sending' AND updated_at < $3))
	ORDER BY next_attempt_at, created_at
	LIMIT $2
)
RETURNING id, task_id, callback_url, bearer_token, payload, attempts, max_attempts`, nowText, limit, staleClaimBefore)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deliveries []federation.A2APushDelivery
	for rows.Next() {
		var delivery federation.A2APushDelivery
		var payload string
		if err := rows.Scan(&delivery.ID, &delivery.TaskID, &delivery.CallbackURL, &delivery.BearerToken, &payload, &delivery.Attempts, &delivery.MaxAttempts); err != nil {
			return nil, err
		}
		delivery.Payload = []byte(payload)
		deliveries = append(deliveries, delivery)
	}
	return deliveries, rows.Err()
}

func (s *A2APushOutboxStore) MarkDelivered(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE a2a_push_outbox
SET status='delivered', updated_at=CURRENT_TIMESTAMP
WHERE id=$1 AND status='sending'`, id)
	return err
}

func (s *A2APushOutboxStore) MarkAttempt(ctx context.Context, id string, nextAttemptAt time.Time, exhausted bool, reason string) error {
	status := "pending"
	if exhausted {
		status = "failed"
	}
	if len(reason) > 200 {
		reason = reason[:200]
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE a2a_push_outbox
SET attempts=attempts+1, status=$2, next_attempt_at=$3, last_error=$4, updated_at=CURRENT_TIMESTAMP
WHERE id=$1 AND status='sending'`, id, status, nextAttemptAt.UTC().Format("2006-01-02 15:04:05"), reason)
	return err
}

func (s *A2APushOutboxStore) Summary(ctx context.Context, tenantID string) (A2APushOutboxSummary, error) {
	return s.summary(ctx, `WHERE tenant_id=$1`, tenantID)
}

func (s *A2APushOutboxStore) SummaryAll(ctx context.Context) (A2APushOutboxSummary, error) {
	return s.summary(ctx, ``, nil)
}

func (s *A2APushOutboxStore) summary(ctx context.Context, predicate string, arg any) (A2APushOutboxSummary, error) {
	var summary A2APushOutboxSummary
	var earliest nullableDatabaseTime
	query := `
SELECT
  COALESCE(SUM(CASE WHEN status='pending' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN status='sending' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN status='delivered' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END), 0),
  MIN(CASE WHEN status='pending' THEN next_attempt_at ELSE NULL END)
FROM a2a_push_outbox
	` + predicate
	var err error
	if predicate == "" {
		err = s.db.QueryRowContext(ctx, query).Scan(&summary.Pending, &summary.Sending, &summary.Delivered, &summary.Failed, &earliest)
	} else {
		err = s.db.QueryRowContext(ctx, query, arg).Scan(&summary.Pending, &summary.Sending, &summary.Delivered, &summary.Failed, &earliest)
	}
	if err != nil {
		return A2APushOutboxSummary{}, err
	}
	if earliest.Valid {
		value := earliest.Time
		summary.EarliestNextAttemptAt = &value
	}
	return summary, nil
}

var _ federation.A2APushOutbox = (*A2APushOutboxStore)(nil)

// ListFailures returns the most recent sanitized failed deliveries for a tenant,
// ordered newest-first with a bounded limit.
func (s *A2APushOutboxStore) ListFailures(ctx context.Context, tenantID string, limit int) ([]A2APushOutboxFailure, error) {
	return s.ListDeliveries(ctx, tenantID, "failed", limit)
}

// ListDeliveries returns the most recent sanitized outbox rows for a tenant,
// optionally filtered by status ("pending", "sending", "delivered", "failed";
// empty selects all), ordered newest-first with a bounded limit. Callback URLs,
// bearer tokens, and payload bodies are never selected.
func (s *A2APushOutboxStore) ListDeliveries(ctx context.Context, tenantID, status string, limit int) ([]A2APushOutboxFailure, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	query := `
SELECT id, task_id, status, attempts, max_attempts, last_error, next_attempt_at, updated_at
FROM a2a_push_outbox
WHERE tenant_id=$1`
	args := []any{tenantID}
	if status != "" {
		query += ` AND status=$2`
		args = append(args, status)
	}
	query += `
ORDER BY updated_at DESC
LIMIT $` + fmt.Sprint(len(args)+1)
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var failures []A2APushOutboxFailure
	for rows.Next() {
		var failure A2APushOutboxFailure
		var nextAttemptAt, updatedAt databaseTime
		var lastError sql.NullString
		if err := rows.Scan(&failure.ID, &failure.TaskID, &failure.Status, &failure.Attempts, &failure.MaxAttempts, &lastError, &nextAttemptAt, &updatedAt); err != nil {
			return nil, err
		}
		failure.LastError = lastError.String
		failure.NextAttemptAt, failure.UpdatedAt = nextAttemptAt.Time, updatedAt.Time
		failures = append(failures, failure)
	}
	return failures, rows.Err()
}
