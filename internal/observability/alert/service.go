package alert

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/tenancy"
)

// Service drives the alert lifecycle and evaluation.
type Service struct {
	store     Store
	notifiers []Notifier
	auditor   Auditor
	ids       contracts.IDGenerator
	clock     contracts.Clock
	events    contracts.EventSink
}

// SetEventSink installs best-effort lifecycle notifications before serving traffic.
func (s *Service) SetEventSink(events contracts.EventSink) { s.events = events }

func NewService(store Store, notifiers []Notifier, auditor Auditor, ids contracts.IDGenerator, clock contracts.Clock) (*Service, error) {
	if store == nil || ids == nil || clock == nil {
		return nil, errors.New("alert dependencies are required")
	}
	return &Service{store: store, notifiers: notifiers, auditor: auditor, ids: ids, clock: clock}, nil
}

// Evaluate fires an alert when a rule's measurement crosses the threshold.
func (s *Service) Evaluate(ctx context.Context, scope tenancy.TenantScope, rule Rule, value float64, windowStart, windowEnd time.Time) (*Alert, bool, error) {
	if !rule.Enabled {
		return nil, false, nil
	}
	if !crosses(rule.Operator, value, rule.Threshold) {
		return nil, false, nil
	}
	id, err := s.ids.New()
	if err != nil {
		return nil, false, err
	}
	now := s.clock.Now()
	alert := &Alert{
		ID: id, TenantID: scope.TenantID, RuleID: rule.ID, Severity: rule.Severity,
		Status: StatusFiring, Message: fmt.Sprintf("%s %s %.2f threshold %.2f", rule.Metric, rule.Operator, value, rule.Threshold),
		Evidence: map[string]string{
			"metric": rule.Metric, "value": fmt.Sprintf("%.4f", value), "threshold": fmt.Sprintf("%.4f", rule.Threshold),
		},
		WindowStart: windowStart, WindowEnd: windowEnd, FiredAt: now,
	}
	if err := s.store.Create(ctx, scope, *alert); err != nil {
		return nil, false, err
	}
	if s.events != nil {
		_ = s.events.Emit(ctx, contracts.DomainEvent{ID: id, Kind: "alert.firing", OccurredAt: now, TenantID: scope.TenantID,
			Attributes: map[string]string{"rule_id": rule.ID, "severity": rule.Severity, "status": StatusFiring}})
	}
	for _, notifier := range s.notifiers {
		if err := notifier.Notify(ctx, *alert); err != nil {
			return alert, true, err
		}
	}
	return alert, true, nil
}

// Ack acknowledges a firing alert.
func (s *Service) Ack(ctx context.Context, scope tenancy.TenantScope, alertID, actorID string) error {
	alert, err := s.store.Get(ctx, scope, alertID)
	if err != nil {
		return err
	}
	if alert.Status != StatusFiring {
		return errors.New("alert is not firing")
	}
	now := s.clock.Now()
	if err := s.store.UpdateStatus(ctx, scope, alertID, StatusAcknowledged, &now); err != nil {
		return err
	}
	return s.audit(ctx, scope, actorID, "alert.ack", alertID)
}

// Silence silences an alert until a deadline.
func (s *Service) Silence(ctx context.Context, scope tenancy.TenantScope, alertID, actorID string, until time.Time) error {
	alert, err := s.store.Get(ctx, scope, alertID)
	if err != nil {
		return err
	}
	if alert.Status != StatusFiring && alert.Status != StatusAcknowledged {
		return errors.New("alert is not actionable")
	}
	if err := s.store.UpdateStatus(ctx, scope, alertID, StatusSilenced, &until); err != nil {
		return err
	}
	return s.audit(ctx, scope, actorID, "alert.silence", alertID)
}

// Resolve marks an alert resolved.
func (s *Service) Resolve(ctx context.Context, scope tenancy.TenantScope, alertID, actorID string) error {
	if _, err := s.store.Get(ctx, scope, alertID); err != nil {
		return err
	}
	now := s.clock.Now()
	if err := s.store.UpdateStatus(ctx, scope, alertID, StatusResolved, &now); err != nil {
		return err
	}
	return s.audit(ctx, scope, actorID, "alert.resolve", alertID)
}

func (s *Service) audit(ctx context.Context, scope tenancy.TenantScope, actorID, action, resourceID string) error {
	if s.events != nil {
		_ = s.events.Emit(ctx, contracts.DomainEvent{ID: resourceID, Kind: action, OccurredAt: s.clock.Now(), TenantID: scope.TenantID})
	}
	if s.auditor == nil {
		return nil
	}
	return s.auditor.RecordAction(ctx, scope, actorID, action, resourceID)
}

func crosses(operator string, value, threshold float64) bool {
	switch operator {
	case "gt":
		return value > threshold
	case "gte":
		return value >= threshold
	case "lt":
		return value < threshold
	case "lte":
		return value <= threshold
	default:
		return false
	}
}
