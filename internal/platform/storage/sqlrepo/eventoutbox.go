package sqlrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
)

const domainEventClaimTimeout = 5 * time.Minute

// OutboxEvent is one claimed domain event ready for delivery.
type OutboxEvent struct {
	ID         string
	Kind       string
	TenantID   string
	ProjectID  string
	RequestID  string
	Attributes map[string]string
	OccurredAt time.Time
	Attempts   int
}

// DomainEventOutboxSummary is the cross-tenant event-outbox status snapshot.
type DomainEventOutboxSummary struct {
	Queued   int
	Claiming int
	Sent     int
	Failed   int
	// EarliestNextAttemptAt is the earliest queued retry, if any.
	EarliestNextAttemptAt *time.Time
}

type DomainEventOutbox struct{ db *sql.DB }

func NewDomainEventOutbox(db *sql.DB) *DomainEventOutbox { return &DomainEventOutbox{db: db} }

func (s *DomainEventOutbox) Enqueue(ctx context.Context, event contracts.DomainEvent) error {
	if event.ID == "" || event.Kind == "" {
		return nil
	}
	attributes, err := json.Marshal(event.Attributes)
	if err != nil {
		return fmt.Errorf("encode domain event attributes: %w", err)
	}
	occurredAt := event.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	// A queued event only becomes due after the claim grace window. Normally the
	// in-process sink delivers it immediately and marks it sent; the future
	// next_attempt_at means the worker only recovers events whose synchronous
	// delivery crashed or was lost mid-flight.
	nextAttemptAt := occurredAt.Add(domainEventClaimTimeout)
	_, err = s.db.ExecContext(ctx, `
INSERT INTO domain_event_outbox(id, kind, tenant_id, project_id, request_id, attributes, occurred_at, next_attempt_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT(id) DO NOTHING`, event.ID, event.Kind, nullString(event.TenantID), nullString(event.ProjectID), nullString(event.RequestID), string(attributes), occurredAt, nextAttemptAt)
	if err != nil {
		return fmt.Errorf("enqueue domain event: %w", err)
	}
	return nil
}

// Due atomically claims up to limit queued/failed events (or reclaims a stale
// claim) and returns them for delivery. Claimed rows move to status
// 'claiming' with an updated lease so a crashed worker does not lose them.
func (s *DomainEventOutbox) Due(ctx context.Context, limit int, now time.Time) ([]OutboxEvent, error) {
	nowText := now.UTC().Format("2006-01-02 15:04:05")
	staleBefore := now.Add(-domainEventClaimTimeout).UTC().Format("2006-01-02 15:04:05")
	nextLease := now.Add(domainEventClaimTimeout).UTC().Format("2006-01-02 15:04:05")
	rows, err := s.db.QueryContext(ctx, `
UPDATE domain_event_outbox
SET status='claiming', next_attempt_at=$1, updated_at=$1
WHERE id IN (
  SELECT id FROM domain_event_outbox
  WHERE (status IN ('queued','failed') AND next_attempt_at <= $2)
     OR (status='claiming' AND updated_at < $3)
  ORDER BY next_attempt_at
  LIMIT $4
)
RETURNING id, kind, tenant_id, project_id, request_id, attributes, occurred_at, attempts`,
		nextLease, nowText, staleBefore, limit)
	if err != nil {
		return nil, fmt.Errorf("claim domain events: %w", err)
	}
	defer rows.Close()
	var events []OutboxEvent
	for rows.Next() {
		var event OutboxEvent
		var attributes string
		var occurredAt databaseTime
		var tenantID, projectID, requestID sql.NullString
		if err := rows.Scan(&event.ID, &event.Kind, &tenantID, &projectID, &requestID, &attributes, &occurredAt, &event.Attempts); err != nil {
			return nil, fmt.Errorf("scan domain event: %w", err)
		}
		event.TenantID, event.ProjectID, event.RequestID = tenantID.String, projectID.String, requestID.String
		event.OccurredAt = occurredAt.Time
		if err := json.Unmarshal([]byte(attributes), &event.Attributes); err != nil {
			return nil, fmt.Errorf("decode domain event attributes: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed domain events: %w", err)
	}
	return events, nil
}

// MarkDelivered finalizes a claimed event as sent.
func (s *DomainEventOutbox) MarkDelivered(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE domain_event_outbox SET status='sent', updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id); err != nil {
		return fmt.Errorf("mark domain event delivered: %w", err)
	}
	return nil
}

// MarkFailed schedules a retry (or final-fails after the attempt budget) with
// an exponential-style next attempt.
func (s *DomainEventOutbox) MarkFailed(ctx context.Context, id string, nextAttemptAt time.Time, exhausted bool) error {
	status := "queued"
	if exhausted {
		status = "failed"
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE domain_event_outbox SET status=$2, next_attempt_at=$3, attempts=attempts+1, updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id, status, nextAttemptAt); err != nil {
		return fmt.Errorf("mark domain event failed: %w", err)
	}
	return nil
}

// SummaryAll returns the cross-tenant event-outbox status snapshot for
// operational monitoring.
func (s *DomainEventOutbox) SummaryAll(ctx context.Context) (DomainEventOutboxSummary, error) {
	var summary DomainEventOutboxSummary
	var earliest nullableDatabaseTime
	query := `
SELECT
  COALESCE(SUM(CASE WHEN status='queued' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN status='claiming' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN status='sent' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END), 0),
  MIN(CASE WHEN status='queued' THEN next_attempt_at ELSE NULL END)
FROM domain_event_outbox`
	if err := s.db.QueryRowContext(ctx, query).Scan(&summary.Queued, &summary.Claiming, &summary.Sent, &summary.Failed, &earliest); err != nil {
		return DomainEventOutboxSummary{}, fmt.Errorf("summarize domain event outbox: %w", err)
	}
	if earliest.Valid {
		value := earliest.Time
		summary.EarliestNextAttemptAt = &value
	}
	return summary, nil
}
