package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// TestGuardrailFastPublishReachesDataPlane proves the fast-publish closed
// loop: a guardrail policy published without a full config republish is
// enforced by the data plane immediately, survives a process restart, and is
// recorded in the audit trail.
func TestGuardrailFastPublishReachesDataPlane(t *testing.T) {
	provider := newTestProviderServer(t)
	dsn := "file:" + filepath.Join(t.TempDir(), "guardrail.db")
	notifications := make(chan string, 8)
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event struct {
			Kind string `json:"kind"`
		}
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Error(err)
		}
		notifications <- event.Kind
		w.WriteHeader(http.StatusNoContent)
	}))
	defer webhook.Close()
	setupBody := map[string]string{
		"username": "admin", "adminPassword": "password-123456", "tenantName": "Acme",
		"providerName": "Test Provider", "providerType": "openai",
		"providerEndpoint": providerEndpoint(provider), "providerSecret": testProviderKey,
		"selectedModel": "gpt-4o-mini",
	}
	boot := func() (*Lite, *httptest.Server, *httptest.Server) {
		lite, err := NewLite(context.Background(), LiteOptions{DSN: dsn, WebhookURL: webhook.URL})
		if err != nil {
			t.Fatal(err)
		}
		return lite, httptest.NewServer(lite.Handler()), httptest.NewServer(lite.GatewayHandler())
	}
	chatBody := map[string]any{
		"model":    "default-chat",
		"messages": []map[string]string{{"role": "user", "content": "tell me about contraband"}},
	}

	lite, admin, gateway := boot()
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
	key := setupResult.VirtualKey

	// Baseline: no guardrail rules yet, the keyword goes through.
	if status, body := doGateway(t, gateway.URL+"/v1/chat/completions", http.MethodPost, key, chatBody); status != http.StatusOK {
		t.Fatalf("baseline chat status = %d body = %s, want 200", status, body)
	}

	status, loginBody, cookies := doJSONFull(t, admin.URL+"/api/admin/session", "POST", map[string]string{
		"username": "admin", "password": "password-123456",
	}, "")
	if status != http.StatusOK {
		t.Fatalf("login status = %d body = %s", status, loginBody)
	}
	var session struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal([]byte(loginBody), &session); err != nil {
		t.Fatal(err)
	}

	publish := func() (int, string) {
		data, err := json.Marshal(map[string]any{
			"id":     "policy",
			"change": "tighten",
			"rules":  []map[string]string{{"id": "deny", "kind": "keyword", "pattern": "contraband", "action": "block"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequest(http.MethodPost, admin.URL+"/api/admin/guardrail/publish", bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Cookie", cookies)
		request.Header.Set("X-CSRF-Token", session.CSRFToken)
		request.Header.Set("X-Reauth-Token", "password-123456")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		out, _ := io.ReadAll(response.Body)
		return response.StatusCode, string(out)
	}

	status, body = publish()
	if status != http.StatusOK {
		t.Fatalf("fast publish status = %d body = %s", status, body)
	}
	var published struct {
		Version int64 `json:"Version"`
	}
	if err := json.Unmarshal([]byte(body), &published); err != nil {
		t.Fatal(err)
	}
	if published.Version != 1 {
		t.Fatalf("published version = %d, want 1", published.Version)
	}

	// The data plane must enforce the new policy without a republish.
	status, body = doGateway(t, gateway.URL+"/v1/chat/completions", http.MethodPost, key, chatBody)
	if status != http.StatusBadRequest {
		t.Fatalf("post-publish chat status = %d body = %s, want 400 (blocked)", status, body)
	}
	select {
	case got := <-notifications:
		if got != "guardrail.match" {
			t.Fatalf("webhook kind = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("missing guardrail webhook")
	}
	benign := map[string]any{
		"model":    "default-chat",
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
	}
	if status, body := doGateway(t, gateway.URL+"/v1/chat/completions", http.MethodPost, key, benign); status != http.StatusOK {
		t.Fatalf("benign chat status = %d body = %s, want 200", status, body)
	}

	// The publish is audited.
	status, body = doJSON(t, admin.URL+"/api/admin/audit", "GET", nil, cookies, "")
	if status != http.StatusOK {
		t.Fatalf("audit status = %d body = %s", status, body)
	}
	if !json.Valid([]byte(body)) {
		t.Fatalf("audit body is not JSON: %s", body)
	}
	var audit []map[string]any
	if err := json.Unmarshal([]byte(body), &audit); err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, record := range audit {
		if record["action"] == "guardrail.fast_publish" {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("audit trail missing guardrail.fast_publish: %s", body)
	}

	// Restart: the fast-published policy must still be enforced.
	gateway.Close()
	admin.Close()
	if err := lite.Close(); err != nil {
		t.Fatal(err)
	}
	lite, admin, gateway = boot()
	defer lite.Close()
	defer admin.Close()
	defer gateway.Close()
	status, body = doGateway(t, gateway.URL+"/v1/chat/completions", http.MethodPost, key, chatBody)
	if status != http.StatusBadRequest {
		t.Fatalf("post-restart chat status = %d body = %s, want 400 (still blocked)", status, body)
	}
	if status, body = doGateway(t, gateway.URL+"/v1/chat/completions", http.MethodPost, key, benign); status != http.StatusOK {
		t.Fatalf("post-restart benign chat status = %d body = %s, want 200", status, body)
	}
}
