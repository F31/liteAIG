package alert

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/tenancy"
)

var errNotFound = errors.New("not found")

// memoryStore is a minimal in-memory Store for service tests.
type memoryStore struct {
	mu     sync.Mutex
	alerts map[string]Alert
}

func newMemoryStore() *memoryStore {
	return &memoryStore{alerts: map[string]Alert{}}
}

func (s *memoryStore) CreateRule(context.Context, tenancy.TenantScope, Rule) error { return nil }
func (s *memoryStore) ListRules(context.Context, tenancy.TenantScope) ([]Rule, error) {
	return nil, nil
}
func (s *memoryStore) Create(_ context.Context, _ tenancy.TenantScope, item Alert) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alerts[item.ID] = item
	return nil
}
func (s *memoryStore) List(_ context.Context, scope tenancy.TenantScope, status string) ([]Alert, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []Alert
	for _, item := range s.alerts {
		if item.TenantID != scope.TenantID {
			continue
		}
		if status != "" && item.Status != status {
			continue
		}
		result = append(result, item)
	}
	return result, nil
}
func (s *memoryStore) UpdateStatus(_ context.Context, _ tenancy.TenantScope, id, status string, at *time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.alerts[id]
	if !ok {
		return errNotFound
	}
	item.Status = status
	switch status {
	case StatusAcknowledged:
		item.AckedAt = at
	case StatusResolved:
		item.ResolvedAt = at
	case StatusSilenced:
		item.SilencedUntil = at
	}
	s.alerts[id] = item
	return nil
}
func (s *memoryStore) Get(_ context.Context, scope tenancy.TenantScope, id string) (*Alert, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.alerts[id]
	if !ok || item.TenantID != scope.TenantID {
		return nil, errNotFound
	}
	copyItem := item
	return &copyItem, nil
}

type ids struct{ next int }

func (g *ids) New() (string, error) { g.next++; return "alert-" + strconv.Itoa(g.next), nil }

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

type auditRecorder struct{ actions []string }

type failingEventSink struct{ events []contracts.DomainEvent }

func (s *failingEventSink) Emit(_ context.Context, event contracts.DomainEvent) error {
	s.events = append(s.events, event)
	return errors.New("notification unavailable")
}

func (a *auditRecorder) RecordAction(_ context.Context, _ tenancy.TenantScope, _ string, action, _ string) error {
	a.actions = append(a.actions, action)
	return nil
}

func TestEvaluateFiresOnThreshold(t *testing.T) {
	store := newMemoryStore()
	auditor := &auditRecorder{}
	service, err := NewService(store, nil, auditor, &ids{}, &clock{now: time.Unix(100, 0)})
	if err != nil {
		t.Fatal(err)
	}
	scope := tenancy.TenantScope{TenantID: "tenant"}
	rule := Rule{ID: "rule-1", TenantID: "tenant", RuleType: "budget", Metric: "budget.usage", Operator: "gt", Threshold: 100, Severity: SeverityHigh, Enabled: true}
	alertResult, fired, err := service.Evaluate(context.Background(), scope, rule, 150, time.Unix(0, 0), time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !fired || alertResult.Status != StatusFiring || alertResult.Severity != SeverityHigh {
		t.Fatalf("alert = %+v fired=%t", alertResult, fired)
	}
	if _, fired, err := service.Evaluate(context.Background(), scope, rule, 50, time.Unix(0, 0), time.Unix(100, 0)); err != nil || fired {
		t.Fatalf("below-threshold fired=%t err=%v", fired, err)
	}
}

func TestAckSilenceResolveLifecycle(t *testing.T) {
	store := newMemoryStore()
	auditor := &auditRecorder{}
	service, err := NewService(store, nil, auditor, &ids{}, &clock{now: time.Unix(100, 0)})
	if err != nil {
		t.Fatal(err)
	}
	events := &failingEventSink{}
	service.SetEventSink(events)
	scope := tenancy.TenantScope{TenantID: "tenant"}
	rule := Rule{ID: "rule-1", TenantID: "tenant", RuleType: "budget", Metric: "budget.usage", Operator: "gt", Threshold: 10, Enabled: true}
	alertResult, _, err := service.Evaluate(context.Background(), scope, rule, 20, time.Unix(0, 0), time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Ack(context.Background(), scope, alertResult.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := service.Silence(context.Background(), scope, alertResult.ID, "admin", time.Unix(200, 0)); err != nil {
		t.Fatal(err)
	}
	if err := service.Resolve(context.Background(), scope, alertResult.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(context.Background(), scope, alertResult.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusResolved || got.ResolvedAt == nil {
		t.Fatalf("alert = %+v", got)
	}
	if len(auditor.actions) != 3 {
		t.Fatalf("audit actions = %v", auditor.actions)
	}
	want := []string{"alert.firing", "alert.ack", "alert.silence", "alert.resolve"}
	if len(events.events) != len(want) {
		t.Fatalf("events = %+v", events.events)
	}
	for i, event := range events.events {
		if event.Kind != want[i] || event.TenantID != scope.TenantID || event.ID != alertResult.ID {
			t.Fatalf("event = %+v", event)
		}
	}
}

func TestLifecycleRejectsInvalidTransition(t *testing.T) {
	store := newMemoryStore()
	service, _ := NewService(store, nil, nil, &ids{}, &clock{now: time.Unix(100, 0)})
	scope := tenancy.TenantScope{TenantID: "tenant"}
	rule := Rule{ID: "rule-1", TenantID: "tenant", Metric: "m", Operator: "gt", Threshold: 1, Enabled: true}
	alertResult, _, err := service.Evaluate(context.Background(), scope, rule, 5, time.Unix(0, 0), time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Resolve(context.Background(), scope, alertResult.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	// Acking a resolved alert must fail.
	if err := service.Ack(context.Background(), scope, alertResult.ID, "admin"); err == nil {
		t.Fatal("ack on resolved alert succeeded")
	}
}

func TestCostAnomalyDetection(t *testing.T) {
	baseline := []AnomalyWindow{
		{WindowStart: time.Unix(0, 0), WindowEnd: time.Unix(60, 0), Cost: 10},
		{WindowStart: time.Unix(60, 0), WindowEnd: time.Unix(120, 0), Cost: 12},
		{WindowStart: time.Unix(120, 0), WindowEnd: time.Unix(180, 0), Cost: 11},
		{WindowStart: time.Unix(180, 0), WindowEnd: time.Unix(240, 0), Cost: 13},
	}
	current := AnomalyWindow{WindowStart: time.Unix(240, 0), WindowEnd: time.Unix(300, 0), Cost: 40}
	result := DetectCostAnomaly(current, baseline, 2.0)
	if !result.Detected {
		t.Fatalf("anomaly not detected: %+v", result)
	}
	if result.WindowStart.Unix() != 240 || result.BaselineMean <= 0 {
		t.Fatalf("evidence window missing: %+v", result)
	}
	// A normal window must not fire.
	normal := AnomalyWindow{WindowStart: time.Unix(240, 0), WindowEnd: time.Unix(300, 0), Cost: 11}
	if DetectCostAnomaly(normal, baseline, 2.0).Detected {
		t.Fatal("normal window flagged as anomaly")
	}
	// Insufficient baseline yields no detection.
	if DetectCostAnomaly(current, baseline[:1], 2.0).Detected {
		t.Fatal("detection with insufficient baseline")
	}
}
