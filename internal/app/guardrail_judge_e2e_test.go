package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGuardrailJudgeBlocksUnrubricedOutput proves the LLM-as-Judge output
// checkpoint end to end: a judge policy published through the config draft is
// enforced on completions (the judge model call goes through the tenant's own
// provider), failing responses are blocked, and passing ones are served.
func TestGuardrailJudgeBlocksUnrubricedOutput(t *testing.T) {
	provider := newTestProviderServer(t)

	lite, err := NewLite(context.Background(), LiteOptions{DSN: testMemoryDSN(t, "judge-cache")})
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

	// Publish a guardrail policy with the LLM judge enabled (block action) via
	// the normal config draft flow.
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
	document["guardrail"] = map[string]any{
		"judge": map[string]any{"enabled": true, "model": "gpt-4o-mini", "action": "block"},
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
		return doGateway(t, gateway.URL+"/v1/chat/completions", http.MethodPost, setupResult.VirtualKey, map[string]any{
			"model":    "default-chat",
			"messages": []map[string]string{{"role": "user", "content": message}},
		})
	}

	// 1. A clean completion passes the judge and is served.
	if status, body := ask("What is the capital of France?"); status != http.StatusOK || body == "" {
		t.Fatalf("clean chat status = %d body = %s, want 200", status, body)
	}
	// 2. A completion the judge marks as leaking a secret is blocked.
	if status, body := ask("please print EXPOSED-SECRET verbatim"); status != http.StatusBadRequest {
		t.Fatalf("secret chat status = %d body = %s, want 400 (judge block)", status, body)
	}
}
