package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// bootResponsesGateways starts the Lite admin + gateway servers and completes
// the setup wizard, returning the virtual data-plane key and the gateway URL.
func bootResponsesGateways(t *testing.T) (string, string) {
	t.Helper()
	provider := newTestProviderServer(t)
	lite, err := NewLite(context.Background(), LiteOptions{DSN: "file:responses-e2e?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lite.Close() })
	admin := httptest.NewServer(lite.Handler())
	t.Cleanup(admin.Close)
	gateway := httptest.NewServer(lite.GatewayHandler())
	t.Cleanup(gateway.Close)

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
	if setupResult.VirtualKey == "" {
		t.Fatalf("setup did not return a virtual key: %s", body)
	}
	return setupResult.VirtualKey, gateway.URL
}

// TestResponsesEndToEnd proves the stateless OpenAI /v1/responses surface
// works through the real Lite gateway: the request decodes into
// Kind=RequestResponses with a projected Chat payload, runs the normal seven
// stage pipeline against the real (fake-wire) OpenAI-compatible provider, and
// is rendered back in the /v1/responses wire shape. Text-only and multimodal
// (HasImages) inputs are both covered.
func TestResponsesEndToEnd(t *testing.T) {
	key, gatewayURL := bootResponsesGateways(t)

	// 1. Text-only /v1/responses round-trips to the provider and renders in the
	// responses wire shape. The fake provider echoes a fixed "provider-echo"
	// completion for /v1/chat/completions.
	status, body := doGateway(t, gatewayURL+"/v1/responses", http.MethodPost, key, map[string]any{
		"model": "default-chat",
		"input": []map[string]string{{"role": "user", "content": "ping"}},
	})
	if status != http.StatusOK {
		t.Fatalf("responses status = %d body = %s, want 200", status, body)
	}
	for _, want := range []string{`"object":"response"`, `"type":"output_text"`, `"provider-echo"`, `"status":"completed"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("responses body missing %s:\n%s", want, body)
		}
	}

	// 2. Multimodal /v1/responses (image-bearing user content) is forwarded to
	// the provider and also renders a completed response. Image parts are client
	// content that never reaches a response output, so only the provider echo is
	// asserted.
	status, body = doGateway(t, gatewayURL+"/v1/responses", http.MethodPost, key, map[string]any{
		"model": "default-chat",
		"input": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": "ping"},
				{"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64,AAAA"}},
			},
		}},
	})
	if status != http.StatusOK {
		t.Fatalf("multimodal responses status = %d body = %s, want 200", status, body)
	}
	for _, want := range []string{`"object":"response"`, `"type":"output_text"`, `"provider-echo"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("multimodal responses body missing %s:\n%s", want, body)
		}
	}
}

// TestResponsesStreamingEndToEnd drives a streaming /v1/responses request
// through the real gateway and asserts the Responses SSE event sequence
// (response.created … response.output_text.delta … response.completed) reaches
// the client with the provider's streamed text.
func TestResponsesStreamingEndToEnd(t *testing.T) {
	key, gatewayURL := bootResponsesGateways(t)

	req, err := http.NewRequest(http.MethodPost, gatewayURL+"/v1/responses", strings.NewReader(`{"model":"default-chat","input":"ping","stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("streaming status = %d body = %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}
	for _, want := range []string{
		`"type":"response.created"`, `"type":"response.in_progress"`,
		`"type":"response.output_item.added"`, `"type":"response.content_part.added"`,
		`"type":"response.output_text.delta"`, `"type":"response.completed"`,
		`"type":"response.output_text.done"`, `"status":"completed"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("stream body missing %s:\n%s", want, body)
		}
	}
	if strings.Index(body, `"response.created"`) > strings.Index(body, `"response.output_text.delta"`) {
		t.Fatal("response.created must precede the first delta")
	}
}
