package app

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/observability/alert"
	"github.com/F31/liteAIG/internal/tenancy"
)

type notificationTenantReader interface {
	FirstTenant(context.Context) (*tenancy.Tenant, error)
}

func notificationSettingsForStartup(ctx context.Context, explicit string, tenants notificationTenantReader, settings alert.NotificationSettingsStore) (*alert.NotificationSettings, error) {
	if explicit != "" {
		return &alert.NotificationSettings{WebhookURL: explicit, Enabled: true}, nil
	}
	if tenants == nil || settings == nil {
		return nil, nil
	}
	tenant, err := tenants.FirstTenant(ctx)
	if errors.Is(err, tenancy.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	stored, err := settings.GetNotificationSettings(ctx, tenancy.TenantScope{TenantID: tenant.ID})
	if errors.Is(err, tenancy.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !stored.Enabled {
		return nil, nil
	}
	return stored, nil
}

// notificationRoute binds one webhook sink to a severity floor.
type notificationRoute struct {
	sink        *alert.WebhookSink
	minSeverity int
}

// notificationSink preserves analytics while isolating webhook latency and
// errors. The bounded queue is best effort: overload drops notifications, not
// requests. Multiple severity-routed targets are supported, and identical
// (kind, rule) notifications are deduplicated within the configured window.
type notificationSink struct {
	analytics contracts.EventSink
	queue     chan contracts.DomainEvent
	mu        sync.Mutex
	closed    bool
	routes    []notificationRoute
	dedup     time.Duration
	dedupHits map[string]time.Time
	now       func() time.Time
}

func newNotificationSink(url string, analytics contracts.EventSink) (contracts.EventSink, func(), error) {
	if url == "" {
		return analytics, func() {}, nil
	}
	sink, closeSink, _, err := newReloadableNotificationSink(&alert.NotificationSettings{WebhookURL: url, Enabled: true, DedupSeconds: 300}, analytics)
	return sink, closeSink, err
}

func settingsForRouting(settings *alert.NotificationSettings) []alert.NotificationTarget {
	if settings == nil || !settings.Enabled {
		return nil
	}
	if len(settings.Targets) > 0 {
		return settings.Targets
	}
	if settings.WebhookURL != "" {
		return []alert.NotificationTarget{{URL: settings.WebhookURL, MinSeverity: alert.SeverityLow}}
	}
	return nil
}

func newReloadableNotificationSink(settings *alert.NotificationSettings, analytics contracts.EventSink) (contracts.EventSink, func(), func(settings *alert.NotificationSettings) error, error) {
	s := &notificationSink{analytics: analytics, queue: make(chan contracts.DomainEvent, 128), dedupHits: map[string]time.Time{}, now: time.Now}
	if err := s.setSettings(settings); err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for event := range s.queue {
			if ctx.Err() != nil {
				return
			}
			s.deduplicateAndRoute(ctx, event)
		}
	}()
	var once sync.Once
	return s, func() {
		once.Do(func() {
			s.closeQueue()
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			select {
			case <-done:
			case <-timer.C:
			}
			cancel()
			<-done
			s.closeRoutes()
		})
	}, s.setSettings, nil
}

// deduplicateAndRoute delivers an event to every route whose severity floor is
// satisfied, suppressing repeat (kind, rule) notifications within the dedup
// window. Events without a severity label go to low-floor targets only.
func (s *notificationSink) deduplicateAndRoute(ctx context.Context, event contracts.DomainEvent) {
	severityRank := alert.SeverityRank(event.Attributes["severity"])
	s.mu.Lock()
	dedup := s.dedup
	now := s.now()
	key := event.Kind
	if severity := event.Attributes["severity"]; severity != "" {
		key += "|" + severity
	}
	if ruleID := event.Attributes["rule_id"]; ruleID != "" {
		key += "|" + ruleID
	}
	if dedup > 0 {
		if last, ok := s.dedupHits[key]; ok && now.Sub(last) < dedup {
			s.mu.Unlock()
			return
		}
		s.dedupHits[key] = now
		for k := range s.dedupHits {
			if now.Sub(s.dedupHits[k]) >= dedup {
				delete(s.dedupHits, k)
			}
		}
	}
	routes := make([]notificationRoute, len(s.routes))
	copy(routes, s.routes)
	s.mu.Unlock()
	for _, route := range routes {
		if severityRank == 0 {
			if route.minSeverity > 1 {
				continue
			}
		} else if severityRank < route.minSeverity {
			continue
		}
		if err := route.sink.Emit(ctx, event); err != nil {
			// Do not log URLs, response bodies, or transport errors containing credentials.
			log.Print("lite: webhook notification delivery failed")
		}
	}
}

func (s *notificationSink) setSettings(settings *alert.NotificationSettings) error {
	var next []notificationRoute
	for _, target := range settingsForRouting(settings) {
		sink, err := alert.NewWebhookSink(target.URL)
		if err != nil {
			s.closeRoutes()
			return err
		}
		next = append(next, notificationRoute{sink: sink, minSeverity: alert.SeverityRank(target.MinSeverity)})
	}
	dedupSeconds := 300
	if settings != nil && settings.DedupSeconds > 0 {
		dedupSeconds = settings.DedupSeconds
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		for _, route := range next {
			route.sink.Close()
		}
		return nil
	}
	s.closeRoutesUnlocked()
	s.routes = next
	s.dedup = time.Duration(dedupSeconds) * time.Second
	return nil
}

func (s *notificationSink) close() {
	s.closeQueue()
	s.closeRoutes()
}

func (s *notificationSink) closeQueue() {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.queue)
	}
	s.mu.Unlock()
}

func (s *notificationSink) closeRoutes() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeRoutesUnlocked()
}

func (s *notificationSink) closeRoutesUnlocked() {
	if s.routes != nil {
		for _, route := range s.routes {
			route.sink.Close()
		}
		s.routes = nil
	}
}

func (s *notificationSink) Emit(ctx context.Context, event contracts.DomainEvent) error {
	err := s.analytics.Emit(ctx, event)
	if notification, ok := alert.NotificationEvent(event); ok {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.closed {
			select {
			case s.queue <- notification:
			default:
				log.Print("lite: webhook notification queue full; notification dropped")
			}
		}
	}
	return err
}
