// Package alert implements the alert lifecycle, typed rules, and notification
// channels with tenant-scoped state.
package alert

import (
	"context"
	"time"

	"github.com/F31/liteAIG/internal/tenancy"
)

// Severity levels.
const (
	SeverityLow      = "low"
	SeverityMedium   = "medium"
	SeverityHigh     = "high"
	SeverityCritical = "critical"
)

// Lifecycle statuses.
const (
	StatusFiring       = "firing"
	StatusAcknowledged = "acknowledged"
	StatusSilenced     = "silenced"
	StatusResolved     = "resolved"
)

// Rule is a typed alert rule.
type Rule struct {
	ID            string
	TenantID      string
	Name          string
	RuleType      string // budget|rate|cost_anomaly
	Metric        string
	Operator      string // gt|gte|lt|lte
	Threshold     float64
	WindowSeconds int64
	Severity      string
	Enabled       bool
	CreatedBy     string
}

// NotificationTarget is one webhook delivery route with a severity floor.
// An alert is delivered to a target when the alert's severity rank is at
// least the target's floor.
type NotificationTarget struct {
	URL         string `json:"url"`
	MinSeverity string `json:"minSeverity"` // low|medium|high|critical
}

// NotificationSettings is the tenant-scoped alert notification configuration.
// WebhookURL retains the legacy single-target setting; Targets carries the
// multi-target routing set. When Targets is empty, WebhookURL (if any) is
// treated as a single target with a "low" severity floor.
type NotificationSettings struct {
	TenantID     string
	WebhookURL   string
	Targets      []NotificationTarget
	DedupSeconds int
	Enabled      bool
	UpdatedBy    string
	UpdatedAt    time.Time
}

// SeverityRank maps a severity label to an integer rank used by notification
// routing. Unknown labels rank as zero (never match a floor above low).
func SeverityRank(severity string) int {
	switch severity {
	case SeverityCritical:
		return 4
	case SeverityHigh:
		return 3
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 1
	default:
		return 0
	}
}

// Alert is one firing incident with evidence metadata.
type Alert struct {
	ID            string
	TenantID      string
	RuleID        string
	Severity      string
	Status        string
	Message       string
	Evidence      map[string]string
	WindowStart   time.Time
	WindowEnd     time.Time
	FiredAt       time.Time
	AckedAt       *time.Time
	ResolvedAt    *time.Time
	SilencedUntil *time.Time
}

// Store persists tenant-scoped alerts and rules.
type Store interface {
	CreateRule(context.Context, tenancy.TenantScope, Rule) error
	ListRules(context.Context, tenancy.TenantScope) ([]Rule, error)
	Create(context.Context, tenancy.TenantScope, Alert) error
	List(context.Context, tenancy.TenantScope, string) ([]Alert, error)
	UpdateStatus(context.Context, tenancy.TenantScope, string, string, *time.Time) error
	Get(context.Context, tenancy.TenantScope, string) (*Alert, error)
}

// NotificationSettingsStore persists tenant-scoped notification targets.
type NotificationSettingsStore interface {
	GetNotificationSettings(context.Context, tenancy.TenantScope) (*NotificationSettings, error)
	UpsertNotificationSettings(context.Context, tenancy.TenantScope, NotificationSettings) error
	DeleteNotificationSettings(context.Context, tenancy.TenantScope) error
}

// Notifier delivers an alert to Webhook or Console.
type Notifier interface {
	Notify(context.Context, Alert) error
}

// Auditor records lifecycle actions (ack/silence/resolve).
type Auditor interface {
	RecordAction(context.Context, tenancy.TenantScope, string, string, string) error
}
