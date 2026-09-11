package alert

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
)

func TestWebhookPayload(t *testing.T) {
	payloads := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Error("expected JSON POST")
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "sensitive") {
			t.Errorf("sensitive data leaked: %s", body)
		}
		var received map[string]any
		if err := json.Unmarshal(body, &received); err != nil {
			t.Error(err)
		}
		payloads <- received
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	sink, err := NewWebhookSink(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	for _, kind := range []string{"alert.firing", "approval.created", "approval.approved", "guardrail.match", "guardrail.retroactive"} {
		event := contracts.DomainEvent{ID: "event", Kind: kind, OccurredAt: time.Unix(100, 0).UTC(), TenantID: "tenant", ProjectID: "project", RequestID: "request",
			Attributes: map[string]string{"rule_id": "rule", "status": "pending", "prompt": "sensitive", "response": "sensitive", "secret": "sensitive", "message": "sensitive", "evidence": "sensitive", "unknown": "sensitive"}}
		if err := sink.Emit(context.Background(), event); err != nil {
			t.Fatal(err)
		}
		received := <-payloads
		if received["kind"] != kind || received["tenant_id"] != "tenant" || received["project_id"] != "project" || received["request_id"] != "request" || received["id"] != "event" || received["occurred_at"] != "1970-01-01T00:01:40Z" {
			t.Fatalf("payload = %#v", received)
		}
		if len(received["attributes"].(map[string]any)) != 2 {
			t.Fatalf("attributes = %v", received["attributes"])
		}
	}
	if err := sink.Emit(context.Background(), contracts.DomainEvent{ID: "system-event", Kind: "system_config.update", OccurredAt: time.Unix(200, 0).UTC(),
		Attributes: map[string]string{"action": "update", "severity": "medium", "actor_id": "sensitive", "secret": "sensitive"}}); err != nil {
		t.Fatal(err)
	}
	received := <-payloads
	if received["kind"] != "system_config.update" || received["tenant_id"] != "" || received["id"] != "system-event" {
		t.Fatalf("system payload = %#v", received)
	}
	attributes := received["attributes"].(map[string]any)
	if len(attributes) != 2 || attributes["action"] != "update" || attributes["severity"] != "medium" {
		t.Fatalf("system attributes = %v", attributes)
	}
	if err := (WebhookNotifier{URL: server.URL}).Notify(context.Background(), Alert{ID: "alert", Message: "sensitive", Evidence: map[string]string{"body": "sensitive"}}); err != nil {
		t.Fatal(err)
	}
}

func TestWebhookStatusAndDisabled(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	sink, err := NewWebhookSink(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	err = sink.Emit(context.Background(), contracts.DomainEvent{Kind: "alert.firing"})
	var statusErr *webhookError
	if !errors.As(err, &statusErr) || err.Error() != "webhook notification failed with status 503" {
		t.Fatalf("error = %v", err)
	}
	if err := sink.Emit(context.Background(), contracts.DomainEvent{Kind: "request.completed"}); err != nil {
		t.Fatal(err)
	}
	disabled, err := NewWebhookSink("")
	if err != nil {
		t.Fatal(err)
	}
	defer disabled.Close()
	if err := disabled.Emit(context.Background(), contracts.DomainEvent{Kind: "alert.firing"}); err != nil {
		t.Fatal(err)
	}
	if err := (WebhookNotifier{}).Notify(context.Background(), Alert{}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestWebhookEgress(t *testing.T) {
	for _, target := range []string{"http://user:pass@example.com/hook", "http://10.0.0.1/hook", "http://169.254.169.254/", "http://192.168.1.1/", "http://[fd00::1]/", "file:///tmp/hook", "https:///missing-host"} {
		if sink, err := NewWebhookSink(target); err == nil {
			sink.Close()
			t.Errorf("accepted %s", target)
		}
	}
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { redirected.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	sink, err := NewWebhookSink(redirect.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	if err := sink.Emit(context.Background(), contracts.DomainEvent{Kind: "alert.firing"}); err == nil {
		t.Fatal("redirect accepted")
	}
	if redirected.Load() != 0 {
		t.Fatal("redirect destination reached")
	}
}

func TestWebhookContextDeadline(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer server.Close()
	defer close(release)
	sink, err := NewWebhookSink(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := sink.Emit(ctx, contracts.DomainEvent{Kind: "guardrail.match"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
}

func TestWebhookOwnTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer server.Close()
	defer close(release)
	sink, err := NewWebhookSink(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	start := time.Now()
	err = sink.Emit(context.Background(), contracts.DomainEvent{Kind: "alert.firing"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 6*time.Second {
		t.Fatalf("delivery took %s", elapsed)
	}
}
