package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// TestStreamingGuardrailCutsAtViolatingChunk proves the three-tier streaming
// wiring (spec §16): a published guardrail rule is enforced per chunk while the
// stream is in flight, cutting the output at the violating chunk instead of
// waiting for the post-assembly output check.
func TestStreamingGuardrailCutsAtViolatingChunk(t *testing.T) {
	provider := newTestProviderServer(t)
	dsn := "file:" + filepath.Join(t.TempDir(), "stream-guardrail.db")
	setupBody := map[string]string{
		"username": "admin", "adminPassword": "password-123456", "tenantName": "Acme",
		"providerName": "Test Provider", "providerType": "openai",
		"providerEndpoint": providerEndpoint(provider), "providerSecret": testProviderKey,
		"selectedModel": "gpt-4o-mini",
	}
	lite, err := NewLite(context.Background(), LiteOptions{DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	defer lite.Close()
	admin := httptest.NewServer(lite.Handler())
	defer admin.Close()
	gateway := httptest.NewServer(lite.GatewayHandler())
	defer gateway.Close()

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

	// Publish a rule that blocks the second streamed chunk ("ok"). The first
	// chunk ("stream-") is held by the buffered layer and must be cut when the
	// violating token arrives, not after the stream completes.
	publishBody, _ := json.Marshal(map[string]any{
		"id":     "policy",
		"change": "tighten",
		"rules":  []map[string]string{{"id": "deny", "kind": "keyword", "pattern": "ok", "action": "block"}},
	})
	request, _ := http.NewRequest(http.MethodPost, admin.URL+"/api/admin/guardrail/publish", bytes.NewReader(publishBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Cookie", cookies)
	request.Header.Set("X-CSRF-Token", session.CSRFToken)
	request.Header.Set("X-Reauth-Token", "password-123456")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("publish status = %d body = %s", response.StatusCode, out)
	}

	// Streaming request through the data plane.
	streamBody := map[string]any{
		"model":    "default-chat",
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
		"stream":   true,
	}
	raw, err := doGatewayStream(t, gateway.URL+"/v1/chat/completions", key, streamBody)
	if err != nil {
		t.Fatalf("stream request failed: %v", err)
	}
	// The violating chunk ("ok") must have been cut: the SSE body may carry the
	// first chunk but must not deliver a completed "stream-ok" as the final
	// output, and the block error must be surfaced.
	if strings.Contains(raw, "stream-ok") {
		t.Fatalf("stream delivered the violating full output: %s", raw)
	}
	// The stream must not terminate cleanly with the blockable content.
	if !strings.Contains(raw, "error") && !strings.Contains(raw, "GUARDRAIL_BLOCKED") && !strings.Contains(raw, "blocked") {
		t.Fatalf("stream did not report the guardrail block: %s", raw)
	}
}
