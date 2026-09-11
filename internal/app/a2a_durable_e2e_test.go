package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// TestA2ADurableTaskAndIdempotentReplay proves the durable outbound-relay task
// state (Work Package 4): a completed A2A relay task is persisted and replayed
// from its stored result, so retrying the same idempotency key never re-hits
// the remote agent — including across a process restart on the same file-backed
// database — while a fresh idempotency key always relays.
func TestA2ADurableTaskAndIdempotentReplay(t *testing.T) {
	var remoteCalls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":"1","result":{"message":{"parts":[{"kind":"text","text":"a2a-echo"}]}}}`)
	}))
	t.Cleanup(remote.Close)

	dsn := "file:" + filepath.Join(t.TempDir(), "a2a-durable.db")
	boot := func() *Lite {
		lite, err := NewLite(context.Background(), LiteOptions{DSN: dsn})
		if err != nil {
			t.Fatal(err)
		}
		return lite
	}

	lite := boot()
	admin := httptest.NewServer(lite.Handler())
	gateway := httptest.NewServer(lite.GatewayHandler())

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
	if setupResult.VirtualKey == "" {
		t.Fatalf("setup did not return a virtual key: %s", body)
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

	// Publish a config where the A2A relay target is an external federated
	// agent reached through the httptest remote, gated by an active verified
	// outbound relationship (the same trust shape Work Package 2 enforces).
	{
		status, body := doJSON(t, admin.URL+"/api/admin/config/drafts", "POST", nil, cookies, session.CSRFToken)
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
			"id": "agent-ext", "tenant_id": tenantID, "project_id": projectID,
			"name": "Ext Agent", "status": "active", "capabilities": []string{"chat"},
		}}
		document["agent_endpoints"] = []map[string]any{{
			"id": "ep-ext", "tenant_id": tenantID, "agent_id": "agent-ext",
			"version": "1", "url": remote.URL, "protocol": "a2a", "capabilities": []string{"chat"},
		}}
		document["federated_agents"] = []map[string]any{{
			"id": "agent-ext", "tenant_id": tenantID, "name": "Ext",
			"external_subject": "ext-subj", "trust_boundary": "external_federated", "status": "active",
		}}
		document["federation_relationships"] = []map[string]any{{
			"id": "rel-1", "tenant_id": tenantID, "external_agent_id": "agent-ext",
			"status": "active", "assurance_level": "high", "has_verified_anchor": true,
			"direction": "outbound", "project_grants": []string{projectID}, "capability_grants": []string{"chat"},
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
	}

	relay := func(messageID, idempotencyKey string) (int, string) {
		payload := map[string]any{
			"messageId": messageID, "role": "user",
			"parts": []map[string]string{{"kind": "text", "text": "hello"}},
			"metadata": map[string]string{
				"a2a.idempotency_key": idempotencyKey, "a2a.delegation.hops": "0",
			},
		}
		return doGateway(t, gateway.URL+"/a2a", http.MethodPost, key, payload)
	}

	// (a) A fresh idempotency key relays once and completes.
	status, firstBody := relay("m-1", "k-1")
	if status != http.StatusOK || !strings.Contains(firstBody, "a2a-echo") {
		t.Fatalf("first relay status = %d body = %s, want 200 + echo", status, firstBody)
	}
	if remoteCalls.Load() != 1 {
		t.Fatalf("remote calls after first relay = %d, want 1", remoteCalls.Load())
	}

	// (b) The same idempotency key is deduplicated: an identical body is served
	// from the stored result and the remote is not called again.
	status, replayed := relay("m-1", "k-1")
	if status != http.StatusOK || replayed != firstBody {
		t.Fatalf("idempotent replay status = %d\nfirst: %s\nreplay: %s", status, firstBody, replayed)
	}
	if remoteCalls.Load() != 1 {
		t.Fatalf("remote calls after idempotent replay = %d, want still 1", remoteCalls.Load())
	}

	// (c) A new idempotency key relays again.
	status, secondKeyBody := relay("m-2", "k-2")
	if status != http.StatusOK || !strings.Contains(secondKeyBody, "a2a-echo") {
		t.Fatalf("second-key relay status = %d body = %s, want 200 + echo", status, secondKeyBody)
	}
	if remoteCalls.Load() != 2 {
		t.Fatalf("remote calls after new key = %d, want 2", remoteCalls.Load())
	}

	// Restart on the same file-backed database: the persisted completed task
	// for k-1 must be replayed from its stored result with no new remote hit.
	gateway.Close()
	admin.Close()
	if err := lite.Close(); err != nil {
		t.Fatal(err)
	}
	lite = boot()
	defer lite.Close()
	gateway = httptest.NewServer(lite.GatewayHandler())
	defer gateway.Close()

	// (d) Post-restart replay of k-1 reuses the stored completion.
	status, afterRestart := relay("m-1", "k-1")
	if status != http.StatusOK || afterRestart != replayed {
		t.Fatalf("post-restart replay status = %d\nstored: %s\nafter:  %s", status, replayed, afterRestart)
	}
	if remoteCalls.Load() != 2 {
		t.Fatalf("remote calls after post-restart replay = %d, want still 2 (persisted completion reused)", remoteCalls.Load())
	}
}
