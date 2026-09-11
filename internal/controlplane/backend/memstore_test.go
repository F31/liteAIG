package backend

import (
	"context"
	"sync"
	"time"

	"github.com/F31/liteAIG/internal/observability/alert"
	"github.com/F31/liteAIG/internal/tenancy"
)

// newMemoryAlertStore is a minimal alert.Store for ControlBackend tests.
func newMemoryAlertStore() *memoryAlertStore {
	return &memoryAlertStore{rules: map[string]alert.Rule{}, notifications: map[string]alert.NotificationSettings{}}
}

type memoryAlertStore struct {
	mu            sync.Mutex
	rules         map[string]alert.Rule
	notifications map[string]alert.NotificationSettings
}

func (s *memoryAlertStore) CreateRule(_ context.Context, scope tenancy.TenantScope, rule alert.Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rule.TenantID = scope.TenantID
	s.rules[rule.ID] = rule
	return nil
}
func (s *memoryAlertStore) ListRules(_ context.Context, scope tenancy.TenantScope) ([]alert.Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []alert.Rule
	for _, rule := range s.rules {
		if rule.TenantID == scope.TenantID {
			result = append(result, rule)
		}
	}
	return result, nil
}
func (s *memoryAlertStore) Create(context.Context, tenancy.TenantScope, alert.Alert) error {
	return nil
}
func (s *memoryAlertStore) List(context.Context, tenancy.TenantScope, string) ([]alert.Alert, error) {
	return nil, nil
}
func (s *memoryAlertStore) UpdateStatus(context.Context, tenancy.TenantScope, string, string, *time.Time) error {
	return nil
}
func (s *memoryAlertStore) Get(context.Context, tenancy.TenantScope, string) (*alert.Alert, error) {
	return nil, nil
}
func (s *memoryAlertStore) GetNotificationSettings(_ context.Context, scope tenancy.TenantScope) (*alert.NotificationSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	settings, ok := s.notifications[scope.TenantID]
	if !ok {
		return nil, tenancy.ErrNotFound
	}
	return &settings, nil
}
func (s *memoryAlertStore) UpsertNotificationSettings(_ context.Context, scope tenancy.TenantScope, settings alert.NotificationSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	settings.TenantID = scope.TenantID
	s.notifications[scope.TenantID] = settings
	return nil
}
func (s *memoryAlertStore) DeleteNotificationSettings(_ context.Context, scope tenancy.TenantScope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.notifications, scope.TenantID)
	return nil
}
