package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestAnalyticsSinkForwardsRedactedEvents proves the ClickHouse analytics sink
// is wired to the data-plane EventSink: a completed request emits a redacted
// "request.completed" event that is flushed (JSONEachRow) to the configured
// endpoint on shutdown.
func TestAnalyticsSinkForwardsRedactedEvents(t *testing.T) {
	provider := newTestProviderServer(t)

	var (
		mu       sync.Mutex
		received []map[string]any
	)
	clickhouse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
			if line == "" {
				continue
			}
			var event map[string]any
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				mu.Unlock()
				http.Error(w, "bad json row", http.StatusBadRequest)
				return
			}
			received = append(received, event)
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(clickhouse.Close)

	lite, err := NewLite(context.Background(), LiteOptions{
		DSN:       "file:analytics-sink?mode=memory&cache=shared",
		Analytics: &AnalyticsOptions{BaseURL: clickhouse.URL, Table: "analytics_events"},
	})
	if err != nil {
		t.Fatal(err)
	}
	admin := httptest.NewServer(lite.Handler())
	gateway := httptest.NewServer(lite.GatewayHandler())
	t.Cleanup(func() { gateway.Close(); admin.Close() })

	setupBody := map[string]string{
		"username": "admin", "adminPassword": "password-123456", "tenantName": "Acme",
		"providerName": "Test Provider", "providerType": "openai",
		"providerEndpoint": providerEndpoint(provider), "providerSecret": testProviderKey,
		"selectedModel": "gpt-4o-mini",
	}
	status, body := doJSON(t, admin.URL+"/api/admin/setup", "POST", setupBody, "", "")
	if status != http.StatusOK {
		t.Fatalf("setup status = %d body = %s", status, body)
	}
	var setupResult struct {
		VirtualKey string `json:"virtualKey"`
	}
	if err := json.Unmarshal([]byte(body), &setupResult); err != nil {
		t.Fatal(err)
	}

	status, body = doGateway(t, gateway.URL+"/v1/chat/completions", http.MethodPost, setupResult.VirtualKey, map[string]any{
		"model":    "default-chat",
		"messages": []map[string]string{{"role": "user", "content": "What is the capital of France?"}},
	})
	if status != http.StatusOK {
		t.Fatalf("chat status = %d body = %s, want 200", status, body)
	}

	// Shutdown flushes the buffered analytics events to the sink.
	if err := lite.Close(); err != nil {
		t.Fatal(err)
	}
	gateway.Close()
	admin.Close()

	mu.Lock()
	defer mu.Unlock()
	var completed []map[string]any
	for _, event := range received {
		if event["Kind"] == "request.completed" || event["kind"] == "request.completed" {
			completed = append(completed, event)
		}
	}
	if len(completed) == 0 {
		t.Fatalf("no request.completed analytics event received; got %d events: %v", len(received), received)
	}
	// Redaction: no sensitive attribute keys may be present.
	for _, event := range completed {
		attrs, _ := event["Attributes"].(map[string]any)
		for key := range attrs {
			switch key {
			case "content", "body", "prompt", "secret", "api_key", "authorization", "password", "token":
				t.Fatalf("sensitive attribute %q leaked into analytics event", key)
			}
		}
	}
}
