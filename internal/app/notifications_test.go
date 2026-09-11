package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/observability/alert"
	"github.com/F31/liteAIG/internal/tenancy"
)

type notificationTenantStub struct {
	tenant *tenancy.Tenant
	err    error
}

func (s notificationTenantStub) FirstTenant(context.Context) (*tenancy.Tenant, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.tenant, nil
}

type notificationSettingsStub struct {
	settings *alert.NotificationSettings
	err      error
}

func (s notificationSettingsStub) GetNotificationSettings(context.Context, tenancy.TenantScope) (*alert.NotificationSettings, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.settings, nil
}
func (s notificationSettingsStub) UpsertNotificationSettings(context.Context, tenancy.TenantScope, alert.NotificationSettings) error {
	return nil
}
func (s notificationSettingsStub) DeleteNotificationSettings(context.Context, tenancy.TenantScope) error {
	return nil
}

type notificationAnalytics struct {
	events []contracts.DomainEvent
	err    error
}

func (s *notificationAnalytics) Emit(_ context.Context, event contracts.DomainEvent) error {
	s.events = append(s.events, event)
	return s.err
}

func TestNotificationFanoutAndDrain(t *testing.T) {
	var received struct {
		Kind       string            `json:"kind"`
		Attributes map[string]string `json:"attributes"`
	}
	receivedDone := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		close(receivedDone)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	analytics := &notificationAnalytics{err: errors.New("analytics failure")}
	sink, closeSink, err := newNotificationSink(server.URL, analytics)
	if err != nil {
		t.Fatal(err)
	}
	defer closeSink()
	event := contracts.DomainEvent{Kind: "guardrail.match", Attributes: map[string]string{"rule_id": "rule", "prompt": "sensitive"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Request lifetime must not cancel queued notifications.
	if err := sink.Emit(ctx, event); !errors.Is(err, analytics.err) {
		t.Fatalf("error = %v", err)
	}
	event.Attributes["rule_id"] = "mutated"
	analytics.err = nil
	if err := sink.Emit(ctx, contracts.DomainEvent{Kind: "request.completed"}); err != nil {
		t.Fatal(err)
	}
	unblock()
	closeSink()
	<-receivedDone
	if received.Kind != "guardrail.match" || received.Attributes["rule_id"] != "rule" || len(received.Attributes) != 1 {
		t.Fatalf("received = %+v", received)
	}
	if len(analytics.events) != 2 {
		t.Fatalf("analytics events = %d", len(analytics.events))
	}
	// Close is idempotent and late Emit cannot panic or send another webhook.
	closeSink()
	if err := sink.Emit(ctx, event); err != nil {
		t.Fatal(err)
	}
}

func TestNotificationSettingsForStartup(t *testing.T) {
	ctx := context.Background()
	stored := notificationSettingsStub{settings: &alert.NotificationSettings{WebhookURL: "https://stored.example/hook", Enabled: true}}
	tenants := notificationTenantStub{tenant: &tenancy.Tenant{ID: "tenant"}}
	explicit, err := notificationSettingsForStartup(ctx, "https://explicit.example/hook", tenants, stored)
	if err != nil || explicit == nil || explicit.WebhookURL != "https://explicit.example/hook" {
		t.Fatalf("explicit = %+v err=%v", explicit, err)
	}
	fromStored, err := notificationSettingsForStartup(ctx, "", tenants, stored)
	if err != nil || fromStored == nil || fromStored.WebhookURL != "https://stored.example/hook" {
		t.Fatalf("stored = %+v err=%v", fromStored, err)
	}
	disabled, err := notificationSettingsForStartup(ctx, "", tenants, notificationSettingsStub{settings: &alert.NotificationSettings{WebhookURL: "https://stored.example/hook", Enabled: false}})
	if err != nil || disabled != nil {
		t.Fatalf("disabled = %+v err=%v", disabled, err)
	}
	noTenant, err := notificationSettingsForStartup(ctx, "", notificationTenantStub{err: tenancy.ErrNotFound}, stored)
	if err != nil || noTenant != nil {
		t.Fatalf("no tenant = %+v err=%v", noTenant, err)
	}
}

func TestReloadableNotificationSinkUpdatesTarget(t *testing.T) {
	ctx := context.Background()
	received := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	sink, closeSink, reload, err := newReloadableNotificationSink(nil, noopEventSink{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeSink()
	if err := reload(&alert.NotificationSettings{WebhookURL: server.URL + "/first", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Emit(ctx, contracts.DomainEvent{Kind: "alert.firing"}); err != nil {
		t.Fatal(err)
	}
	select {
	case path := <-received:
		if path != "/first" {
			t.Fatalf("path = %q", path)
		}
	case <-time.After(time.Second):
		t.Fatal("notification not delivered after reload")
	}
	if err := reload(&alert.NotificationSettings{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Emit(ctx, contracts.DomainEvent{Kind: "alert.firing"}); err != nil {
		t.Fatal(err)
	}
	select {
	case path := <-received:
		t.Fatalf("notification delivered after disable to %q", path)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestNotificationDisabledAndBlocked(t *testing.T) {
	analytics := &notificationAnalytics{}
	sink, closeSink, err := newNotificationSink("", analytics)
	if err != nil || sink != analytics {
		t.Fatalf("disabled sink = %v, %v", sink, err)
	}
	closeSink()
	if _, _, err := newNotificationSink("http://169.254.169.254/", analytics); err == nil {
		t.Fatal("metadata destination accepted")
	}
	if lite, err := NewLite(context.Background(), LiteOptions{DSN: "file:webhook-blocked?mode=memory&cache=shared", WebhookURL: "http://10.0.0.1/"}); err == nil {
		_ = lite.Close()
		t.Fatal("app accepted blocked webhook")
	}
}

func TestNotificationQueueDoesNotBlock(t *testing.T) {
	// No consumer: fill the queue to prove overload cannot block request handling.
	sink := &notificationSink{analytics: noopEventSink{}, queue: make(chan contracts.DomainEvent, 1), now: time.Now}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2; i++ {
			_ = sink.Emit(context.Background(), contracts.DomainEvent{Kind: "alert.firing"})
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Emit blocked on full queue")
	}
}

func TestNotificationSeverityRoutingAndDedup(t *testing.T) {
	type receivedEvent struct {
		Kind  string            `json:"kind"`
		Attrs map[string]string `json:"attributes"`
	}
	lowHits := make(chan receivedEvent, 4)
	highHits := make(chan receivedEvent, 4)
	newServer := func(hits chan receivedEvent) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var ev receivedEvent
			if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
				t.Error(err)
			}
			hits <- ev
			w.WriteHeader(http.StatusNoContent)
		}))
	}
	low := newServer(lowHits)
	defer low.Close()
	high := newServer(highHits)
	defer high.Close()

	settings := &alert.NotificationSettings{
		WebhookURL: low.URL,
		Targets: []alert.NotificationTarget{
			{URL: low.URL, MinSeverity: alert.SeverityLow},
			{URL: high.URL, MinSeverity: alert.SeverityHigh},
		},
		DedupSeconds: 60,
		Enabled:      true,
	}
	sink, closeSink, _, err := newReloadableNotificationSink(settings, noopEventSink{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeSink()
	ctx := context.Background()

	fire := func(kind string, severity string) {
		attrs := map[string]string{"rule_id": "rule-1"}
		if severity != "" {
			attrs["severity"] = severity
		}
		if err := sink.Emit(ctx, contracts.DomainEvent{Kind: kind, Attributes: attrs}); err != nil {
			t.Fatal(err)
		}
	}
	fire("alert.firing", alert.SeverityLow)

	select {
	case <-lowHits:
	case <-time.After(time.Second):
		t.Fatal("low target missed for severity=low")
	}
	select {
	case <-highHits:
		t.Fatal("high target received severity=low")
	case <-time.After(120 * time.Millisecond):
	}

	fire("alert.firing", alert.SeverityHigh)
	select {
	case <-lowHits:
	case <-time.After(time.Second):
		t.Fatal("low target missed for severity=high")
	}
	select {
	case <-highHits:
	case <-time.After(time.Second):
		t.Fatal("high target missed for severity=high")
	}

	// Missing severity routes to low-floor targets only.
	fire("guardrail.match", "")
	select {
	case <-lowHits:
	case <-time.After(time.Second):
		t.Fatal("unlabeled event missed low target")
	}
	select {
	case <-highHits:
		t.Fatal("high target received unlabeled event")
	case <-time.After(120 * time.Millisecond):
	}

	// Same (kind, severity, rule) within the dedup window is suppressed.
	fireWithRule := func(kind, severity, rule string) {
		attrs := map[string]string{"rule_id": rule}
		if severity != "" {
			attrs["severity"] = severity
		}
		if err := sink.Emit(ctx, contracts.DomainEvent{Kind: kind, Attributes: attrs}); err != nil {
			t.Fatal(err)
		}
	}
	fireWithRule("alert.firing", alert.SeverityHigh, "rule-2")
	fireWithRule("alert.firing", alert.SeverityHigh, "rule-2")
	select {
	case <-highHits:
		select {
		case <-highHits:
			t.Fatal("duplicate within dedup window delivered twice")
		case <-time.After(120 * time.Millisecond):
		}
	case <-time.After(time.Second):
		t.Fatal("first post-dedup delivery missing")
	}
}

func TestNotificationShutdownBound(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
	}))
	defer server.Close()
	defer close(release)
	sink, closeSink, err := newNotificationSink(server.URL, noopEventSink{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeSink()
	_ = sink.Emit(context.Background(), contracts.DomainEvent{Kind: "alert.firing"})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	// More than one stalled delivery must not multiply the shutdown deadline.
	for i := 0; i < 3; i++ {
		_ = sink.Emit(context.Background(), contracts.DomainEvent{Kind: "guardrail.match"})
	}
	start := time.Now()
	closeSink()
	if elapsed := time.Since(start); elapsed > 6*time.Second {
		t.Fatalf("shutdown took %s", elapsed)
	}
	_ = sink.Emit(context.Background(), contracts.DomainEvent{Kind: "alert.firing"})
}
