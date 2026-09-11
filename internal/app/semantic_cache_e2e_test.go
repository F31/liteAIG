package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSemanticCacheServesSimilarPrompt proves the Stage 3 fallback: after an
// exact-cache miss, a prompt similar enough to a cached one is served from the
// exact store under the semantic source (no upstream chat call), while
// dissimilar prompts still execute.
func TestSemanticCacheServesSimilarPrompt(t *testing.T) {
	provider := newTestProviderServer(t)

	lite, err := NewLite(context.Background(), LiteOptions{DSN: testMemoryDSN(t, "semantic-cache")})
	if err != nil {
		t.Fatal(err)
	}
	admin := httptest.NewServer(lite.Handler())
	gateway := httptest.NewServer(lite.GatewayHandler())
	t.Cleanup(func() { gateway.Close(); admin.Close(); _ = lite.Close() })

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

	// Enable the exact cache (5 min TTL) plus the semantic fallback on the
	// tenant's own embedding endpoint (the test provider serves /v1/embeddings
	// with a deterministic keyword vector).
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
	document["cache"] = map[string]any{
		"enabled":           true,
		"ttl_seconds":       300,
		"namespace_version": 1,
		"semantic":          map[string]any{"enabled": true, "model": "gpt-4o-mini", "threshold": 0.85},
	}
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

	ask := func(message string) (int, string) {
		payload, _ := json.Marshal(map[string]any{
			"model":    "default-chat",
			"messages": []map[string]string{{"role": "user", "content": message}},
		})
		request, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(string(payload)))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+setupResult.VirtualKey)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var chat struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		_ = json.NewDecoder(response.Body).Decode(&chat)
		content := ""
		if len(chat.Choices) > 0 {
			content = chat.Choices[0].Message.Content
		}
		return response.StatusCode, content
	}

	// 1. First call executes upstream and populates the cache.
	if status, content := ask("What is the capital of France?"); status != http.StatusOK || content != "provider-echo" {
		t.Fatalf("first call status=%d content=%s, want 200 provider-echo", status, content)
	}
	// 2. Identical call is an exact-cache hit.
	if status, content := ask("What is the capital of France?"); status != http.StatusOK || content != "provider-echo" {
		t.Fatalf("exact call status=%d content=%s", status, content)
	}
	// 3. Paraphrased call: exact miss, semantic hit (same keyword vector),
	// served from the cache without a new upstream call.
	if status, content := ask("What is the capital city of France?"); status != http.StatusOK || content != "provider-echo" {
		t.Fatalf("semantic call status=%d content=%s", status, content)
	}
	// 4. Dissimilar prompt executes normally.
	if status, content := ask("Tell me a joke about routers"); status != http.StatusOK || content != "provider-echo" {
		t.Fatalf("dissimilar call status=%d content=%s", status, content)
	}

	// The ledger attributes each request to its serving path.
	status, body = doJSON(t, admin.URL+"/api/admin/requests", "GET", nil, cookies, "")
	if status != http.StatusOK {
		t.Fatalf("requests status = %d body = %s", status, body)
	}
	var records []map[string]any
	if err := json.Unmarshal([]byte(body), &records); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for _, record := range records {
		if record["source"] != "playground" {
			sources = append(sources, record["source"].(string))
		}
	}
	counts := map[string]int{}
	for _, source := range sources {
		counts[source]++
	}
	if counts["gateway"] != 2 || counts["cache"] != 1 || counts["semantic_cache"] != 1 {
		t.Fatalf("source attribution = %v, want gateway=2 cache=1 semantic_cache=1 (raw %v)", counts, sources)
	}
}
