package sqlrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/F31/liteAIG/internal/observability/alert"
	"github.com/F31/liteAIG/internal/tenancy"
)

// AlertStore persists tenant-scoped alert rules and incidents.
type AlertStore struct{ db *sql.DB }

func NewAlertStore(db *sql.DB) *AlertStore { return &AlertStore{db: db} }

func (s *AlertStore) CreateRule(ctx context.Context, scope tenancy.TenantScope, rule alert.Rule) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if rule.TenantID != scope.TenantID {
		return tenancy.ErrScopeMismatch
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO alert_rules(id, tenant_id, name, rule_type, metric, operator, threshold, window_seconds, severity, enabled, created_by, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		rule.ID, rule.TenantID, rule.Name, rule.RuleType, rule.Metric, rule.Operator, rule.Threshold,
		rule.WindowSeconds, rule.Severity, rule.Enabled, rule.CreatedBy, time.Now().UTC(),
	)
	return err
}

func (s *AlertStore) ListRules(ctx context.Context, scope tenancy.TenantScope) ([]alert.Rule, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, tenant_id, name, rule_type, metric, operator, threshold, window_seconds, severity, enabled, created_by FROM alert_rules WHERE tenant_id=$1`, scope.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []alert.Rule
	for rows.Next() {
		var rule alert.Rule
		if err := rows.Scan(&rule.ID, &rule.TenantID, &rule.Name, &rule.RuleType, &rule.Metric, &rule.Operator, &rule.Threshold, &rule.WindowSeconds, &rule.Severity, &rule.Enabled, &rule.CreatedBy); err != nil {
			return nil, err
		}
		result = append(result, rule)
	}
	return result, rows.Err()
}

func (s *AlertStore) Create(ctx context.Context, scope tenancy.TenantScope, alertRecord alert.Alert) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	evidence, err := json.Marshal(alertRecord.Evidence)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO alerts(id, tenant_id, rule_id, severity, status, message, evidence, window_start, window_end, fired_at, acked_at, resolved_at, silenced_until)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		alertRecord.ID, alertRecord.TenantID, alertRecord.RuleID, alertRecord.Severity, alertRecord.Status,
		alertRecord.Message, string(evidence), alertRecord.WindowStart, alertRecord.WindowEnd, alertRecord.FiredAt,
		alertRecord.AckedAt, alertRecord.ResolvedAt, alertRecord.SilencedUntil,
	)
	return err
}

func (s *AlertStore) List(ctx context.Context, scope tenancy.TenantScope, status string) ([]alert.Alert, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	query := `SELECT id, tenant_id, rule_id, severity, status, message, evidence, window_start, window_end, fired_at, acked_at, resolved_at, silenced_until FROM alerts WHERE tenant_id=$1`
	args := []any{scope.TenantID}
	if status != "" {
		query += ` AND status=$2`
		args = append(args, status)
	}
	query += ` ORDER BY fired_at DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []alert.Alert
	for rows.Next() {
		var item alert.Alert
		var evidence string
		var acked, resolved, silenced nullableDatabaseTime
		var windowStart, windowEnd, firedAt databaseTime
		if err := rows.Scan(&item.ID, &item.TenantID, &item.RuleID, &item.Severity, &item.Status, &item.Message, &evidence, &windowStart, &windowEnd, &firedAt, &acked, &resolved, &silenced); err != nil {
			return nil, err
		}
		item.WindowStart, item.WindowEnd, item.FiredAt = windowStart.Time, windowEnd.Time, firedAt.Time
		decodeJSON([]byte(evidence), &item.Evidence)
		if acked.Valid {
			value := acked.Time
			item.AckedAt = &value
		}
		if resolved.Valid {
			value := resolved.Time
			item.ResolvedAt = &value
		}
		if silenced.Valid {
			value := silenced.Time
			item.SilencedUntil = &value
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *AlertStore) UpdateStatus(ctx context.Context, scope tenancy.TenantScope, alertID, status string, at *time.Time) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	now := time.Now().UTC()
	if at == nil {
		at = &now
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE alerts
SET status=$3, acked_at=CASE WHEN $3='acknowledged' THEN $4 ELSE acked_at END,
    resolved_at=CASE WHEN $3='resolved' THEN $4 ELSE resolved_at END,
    silenced_until=CASE WHEN $3='silenced' THEN $4 ELSE silenced_until END
WHERE tenant_id=$1 AND id=$2`, scope.TenantID, alertID, status, *at)
	return err
}

func (s *AlertStore) Get(ctx context.Context, scope tenancy.TenantScope, alertID string) (*alert.Alert, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	var item alert.Alert
	var evidence string
	var acked, resolved, silenced nullableDatabaseTime
	var windowStart, windowEnd, firedAt databaseTime
	err := s.db.QueryRowContext(ctx, `SELECT id, tenant_id, rule_id, severity, status, message, evidence, window_start, window_end, fired_at, acked_at, resolved_at, silenced_until FROM alerts WHERE tenant_id=$1 AND id=$2`, scope.TenantID, alertID).Scan(&item.ID, &item.TenantID, &item.RuleID, &item.Severity, &item.Status, &item.Message, &evidence, &windowStart, &windowEnd, &firedAt, &acked, &resolved, &silenced)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tenancy.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get alert: %w", err)
	}
	item.WindowStart, item.WindowEnd, item.FiredAt = windowStart.Time, windowEnd.Time, firedAt.Time
	decodeJSON([]byte(evidence), &item.Evidence)
	if acked.Valid {
		value := acked.Time
		item.AckedAt = &value
	}
	if resolved.Valid {
		value := resolved.Time
		item.ResolvedAt = &value
	}
	if silenced.Valid {
		value := silenced.Time
		item.SilencedUntil = &value
	}
	return &item, nil
}

func (s *AlertStore) GetNotificationSettings(ctx context.Context, scope tenancy.TenantScope) (*alert.NotificationSettings, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	var settings alert.NotificationSettings
	var updatedBy sql.NullString
	var updatedAt databaseTime
	var targets string
	err := s.db.QueryRowContext(ctx, `
SELECT tenant_id, webhook_url, enabled, updated_by, updated_at, targets, dedup_seconds
FROM notification_settings
WHERE tenant_id=$1`, scope.TenantID).Scan(&settings.TenantID, &settings.WebhookURL, &settings.Enabled, &updatedBy, &updatedAt, &targets, &settings.DedupSeconds)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tenancy.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get notification settings: %w", err)
	}
	settings.UpdatedBy = updatedBy.String
	settings.UpdatedAt = updatedAt.Time
	if targets != "" {
		decodeJSON([]byte(targets), &settings.Targets)
	} else if settings.WebhookURL != "" {
		// Legacy single-target rows synthesize the primary webhook as a low
		// floor, so severity routing never drops the historical target.
		settings.Targets = []alert.NotificationTarget{{URL: settings.WebhookURL, MinSeverity: alert.SeverityLow}}
	}
	return &settings, nil
}

func (s *AlertStore) UpsertNotificationSettings(ctx context.Context, scope tenancy.TenantScope, settings alert.NotificationSettings) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if settings.TenantID != scope.TenantID {
		return tenancy.ErrScopeMismatch
	}
	updatedBy := sql.NullString{String: settings.UpdatedBy, Valid: settings.UpdatedBy != ""}
	targets, err := json.Marshal(settings.Targets)
	if err != nil {
		return fmt.Errorf("encode notification targets: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO notification_settings(tenant_id, webhook_url, enabled, updated_by, updated_at, targets, dedup_seconds)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT(tenant_id) DO UPDATE SET
    webhook_url=excluded.webhook_url,
    enabled=excluded.enabled,
    updated_by=excluded.updated_by,
    updated_at=excluded.updated_at,
    targets=excluded.targets,
    dedup_seconds=excluded.dedup_seconds`, settings.TenantID, settings.WebhookURL, settings.Enabled, updatedBy, settings.UpdatedAt, string(targets), settings.DedupSeconds)
	return err
}

func (s *AlertStore) DeleteNotificationSettings(ctx context.Context, scope tenancy.TenantScope) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM notification_settings WHERE tenant_id=$1`, scope.TenantID)
	return err
}

var _ alert.Store = (*AlertStore)(nil)
var _ alert.NotificationSettingsStore = (*AlertStore)(nil)
