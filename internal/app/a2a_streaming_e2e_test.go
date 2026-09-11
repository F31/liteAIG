package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestA2AInboundStreamingProfileAndPushCallback proves the end-to-end Work
// Package 5 wire: (a) an inbound /a2a message/send that requests the streaming
// profile (Accept: text/event-stream) is answered with the LiteAIG A2A SSE
// events (one message frame with the completed agent reply, then a completed
// frame) and never blocks the relay, and (b) a blocking message/send carrying a
// pushNotificationConfig stores a durable, signed callback and retries after a
// transient callback failure.
func TestA2AInboundStreamingProfileAndPushCallback(t *testing.T) {
	var relayHits atomic.Int32
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relayHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":"1","result":{"message":{"parts":[{"kind":"text","text":"relay-streamed"}]}}}`)
	}))
	t.Cleanup(relay.Close)

	callbackHits := make(chan string, 1)
	var callbackAttempts atomic.Int32
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := callbackAttempts.Add(1)
		if r.Header.Get("Authorization") != "Bearer cb-token" {
			t.Errorf("callback Authorization = %q, want Bearer cb-token", r.Header.Get("Authorization"))
		}
		if !strings.HasPrefix(r.Header.Get("X-LiteAIG-A2A-Push-Signature"), "sha256=") || r.Header.Get("X-LiteAIG-A2A-Push-Timestamp") == "" {
			t.Errorf("callback is missing A2A push signature headers")
		}
		if attempt == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		select {
		case callbackHits <- string(raw):
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(callback.Close)

	gateway, key := bootGatewayWithLocalA2A(t, relay.URL)

	// (a) Streaming profile.
	relayHits.Store(0)
	payload := `{"jsonrpc":"2.0","id":"s1","method":"message/send","params":{"message":{"messageId":"m-stream","role":"user","parts":[{"kind":"text","text":"hello stream"}]}}}`
	status, contentType, sseBody := postGatewayRaw(t, gateway.URL+"/a2a", key, payload, "text/event-stream")
	if status != http.StatusOK || !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("streaming status=%d content-type=%q body=%s", status, contentType, sseBody)
	}
	if !strings.Contains(sseBody, `"type":"message"`) || !strings.Contains(sseBody, "relay-streamed") {
		t.Fatalf("streaming body missing the message event:\n%s", sseBody)
	}
	if !strings.Contains(sseBody, `"type":"completed"`) {
		t.Fatalf("streaming body missing the completed event:\n%s", sseBody)
	}
	if relayHits.Load() != 1 {
		t.Fatalf("relay hits = %d, want 1", relayHits.Load())
	}

	// (b) Blocking push callback.
	relayHits.Store(0)
	pushPayload := fmt.Sprintf(`{"jsonrpc":"2.0","id":"s2","method":"message/send","params":{"message":{"messageId":"m-push","role":"user","parts":[{"kind":"text","text":"hello push"}]},"pushNotificationConfig":{"url":%q,"token":"cb-token"}}}`, callback.URL)
	status, body := postGatewayRawShort(t, gateway.URL+"/a2a", key, pushPayload)
	if status != http.StatusOK || !strings.Contains(body, "relay-streamed") {
		t.Fatalf("push relay status=%d body=%s, want 200 + reply", status, body)
	}
	if relayHits.Load() != 1 {
		t.Fatalf("relay hits = %d, want 1", relayHits.Load())
	}
	select {
	case raw := <-callbackHits:
		var envelope struct {
			MessageID string `json:"messageId"`
			Task      struct {
				Status string `json:"status"`
			} `json:"task"`
			Result struct {
				Message struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"message"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
			t.Fatalf("callback body is not JSON: %v\n%s", err, raw)
		}
		if envelope.MessageID != "m-push" || envelope.Task.Status != "completed" {
			t.Fatalf("callback messageId/task = %q/%q", envelope.MessageID, envelope.Task.Status)
		}
		if len(envelope.Result.Message.Parts) != 1 || envelope.Result.Message.Parts[0].Text != "relay-streamed" {
			t.Fatalf("callback result parts = %+v", envelope.Result.Message.Parts)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("push callback was not delivered")
	}
	if callbackAttempts.Load() < 2 {
		t.Fatalf("callback attempts = %d, want durable retry", callbackAttempts.Load())
	}
}

// TestA2AInboundStreamingRelaysIncrementalDeltas proves the streaming relay no
// longer buffers the whole peer reply: a peer that genuinely streams SSE deltas
// causes the inbound /a2a client to receive one `type: message` event per remote
// delta followed by a single `type: completed` event, with exactly one outbound
// POST.
func TestA2AInboundStreamingRelaysIncrementalDeltas(t *testing.T) {
	var relayHits atomic.Int32
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relayHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		io.WriteString(w, `data: {"type":"message","message":{"parts":[{"kind":"text","text":"Hel"}]}}`+"\n\n")
		flusher.Flush()
		io.WriteString(w, `data: {"type":"message","message":{"parts":[{"kind":"text","text":"lo"}]}}`+"\n\n")
		flusher.Flush()
		io.WriteString(w, `data: {"type":"completed"}`+"\n\n")
		flusher.Flush()
	}))
	t.Cleanup(relay.Close)

	gateway, key := bootGatewayWithLocalA2A(t, relay.URL)
	relayHits.Store(0)
	payload := `{"jsonrpc":"2.0","id":"s1","method":"message/send","params":{"message":{"messageId":"m-inc","role":"user","parts":[{"kind":"text","text":"stream this"}]}}}`
	status, _, sseBody := postGatewayRaw(t, gateway.URL+"/a2a", key, payload, "text/event-stream")
	if status != http.StatusOK {
		t.Fatalf("streaming status=%d body=%s", status, sseBody)
	}
	if relayHits.Load() != 1 {
		t.Fatalf("relay hits = %d, want 1", relayHits.Load())
	}
	if got := strings.Count(sseBody, `"type":"message"`); got != 2 {
		t.Fatalf("message frames = %d, want 2 (one per delta):\n%s", got, sseBody)
	}
	if !strings.Contains(sseBody, `"text":"Hel"`) || !strings.Contains(sseBody, `"text":"lo"`) {
		t.Fatalf("incremental deltas missing:\n%s", sseBody)
	}
	if got := strings.Count(sseBody, `"type":"completed"`); got != 1 {
		t.Fatalf("completed frames = %d, want 1:\n%s", got, sseBody)
	}
	// The buffered whole-reply spelling must NOT appear: each remote delta is
	// its own event, not one concatenated event.
	if strings.Contains(sseBody, `"text":"Hello"`) {
		t.Fatalf("relay buffered deltas into one event:\n%s", sseBody)
	}
}

// sanitizeTestName turns a Go test name (which may contain '/' for subtests)
// into a safe SQLite shared-cache id.
func sanitizeTestName(name string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name)
}

// bootGatewayWithLocalA2A boots a Lite instance, completes setup + admin
// login, and publishes a config whose A2A relay target is the plain local agent
// reached through remoteURL (no federated relationship needed). It returns the
// gateway test server and the data-plane virtual key.
func bootGatewayWithLocalA2A(t *testing.T, remoteURL string) (*httptest.Server, string) {
	t.Helper()
	// A per-test shared-cache memory database isolates each test's Lite state
	// (tenant, agents, relay target, outbox). Reusing one DSN across tests would
	// leak state between them and flake under heavy parallel load.
	dsn := "file:a2a-streaming-" + sanitizeTestName(t.Name()) + "?mode=memory&cache=shared"
	lite, err := NewLite(context.Background(), LiteOptions{DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lite.Close() })
	admin := httptest.NewServer(lite.Handler())
	gateway := httptest.NewServer(lite.GatewayHandler())
	t.Cleanup(func() { gateway.Close(); admin.Close() })

	setupBody := map[string]string{
		"username": "admin", "adminPassword": "password-123456", "tenantName": "Acme",
		"providerName": "Test Provider", "providerType": "openai",
		"providerEndpoint": providerEndpoint(newTestProviderServer(t)), "providerSecret": testProviderKey,
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

	status, body = doJSON(t, admin.URL+"/api/admin/config/drafts", "POST", nil, cookies, session.CSRFToken)
	if status != http.StatusOK {
		t.Fatalf("create draft status = %d body = %s", status, body)
	}
	var draft struct {
		ID       string          `json:"ID"`
		Revision int64           `json:"Revision"`
		Config   json.RawMessage `json:"Config"`
	}
	if err := json.Unmarshal([]byte(body), &draft); err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(draft.Config, &document); err != nil {
		t.Fatal(err)
	}
	tenantID := document["tenant"].(map[string]any)["id"].(string)
	projectID := document["projects"].([]any)[0].(map[string]any)["id"].(string)
	document["agents"] = []map[string]any{{
		"id": "agent-local", "tenant_id": tenantID, "project_id": projectID,
		"name": "Local Agent", "status": "active", "capabilities": []string{"chat"},
	}}
	document["agent_endpoints"] = []map[string]any{{
		"id": "ep-local", "tenant_id": tenantID, "agent_id": "agent-local",
		"version": "1", "url": remoteURL, "protocol": "a2a", "capabilities": []string{"chat"},
	}}
	updated, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	status, body = doJSON(t, admin.URL+"/api/admin/config/drafts/"+draft.ID, "PUT", map[string]any{"revision": draft.Revision, "config": json.RawMessage(updated)}, cookies, session.CSRFToken)
	if status != http.StatusOK {
		t.Fatalf("update draft status = %d body = %s", status, body)
	}
	var updatedDraft struct {
		Revision int64 `json:"Revision"`
	}
	if err := json.Unmarshal([]byte(body), &updatedDraft); err != nil {
		t.Fatal(err)
	}
	status, body = doJSON(t, admin.URL+"/api/admin/config/drafts/"+draft.ID+"/publish", "POST", map[string]int64{"revision": updatedDraft.Revision}, cookies, session.CSRFToken)
	if status != http.StatusOK {
		t.Fatalf("publish status = %d body = %s", status, body)
	}
	return gateway, key
}

// postGatewayRaw performs a gateway POST with a caller-set Accept header and
// returns status, response Content-Type, and the raw body.
func postGatewayRaw(t *testing.T, url, key, body, accept string) (int, string, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(raw)
}

// postGatewayRawShort is the non-streaming variant returning status + body.
func postGatewayRawShort(t *testing.T, url, key, body string) (int, string) {
	t.Helper()
	status, _, raw := postGatewayRaw(t, url, key, body, "")
	return status, raw
}
