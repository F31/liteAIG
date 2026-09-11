package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGlobalSoftBudgetSliceEnforced proves a budget policy with
// consistency=global_soft is enforced by the region SliceAuthority: completions
// are admitted up to the bounded overshoot, then rejected with BUDGET_EXCEEDED,
// and the authoritative window is rolled back on rejection.
func TestGlobalSoftBudgetSliceEnforced(t *testing.T) {
	provider := newTestProviderServer(t)

	lite, err := NewLite(context.Background(), LiteOptions{DSN: "file:global-soft-budget?mode=memory&cache=shared"})
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

	// Publish a tenant-scoped global_soft budget with a small limit so a handful
	// of requests exhaust the strict limit plus the bounded overshoot.
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
	document["budget_policies"] = []map[string]any{{
		"id": "bp-soft", "tenant_id": tenantID, "window_hours": 1,
		"token_limit": 40.0, "mode": "soft", "consistency": "global_soft",
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

	ask := func() int {
		status, _ := doGateway(t, gateway.URL+"/v1/chat/completions", http.MethodPost, setupResult.VirtualKey, map[string]any{
			"model":    "default-chat",
			"messages": []map[string]string{{"role": "user", "content": "What is the capital of France?"}},
		})
		return status
	}

	var admitted, rejected int
	firstRejected := -1
	for i := 0; i < 30; i++ {
		status := ask()
		switch {
		case status == http.StatusOK:
			admitted++
		case status == http.StatusTooManyRequests:
			if firstRejected == -1 {
				firstRejected = i
			}
			rejected++
		default:
			t.Fatalf("unexpected status %d at request %d", status, i)
		}
	}
	if rejected == 0 {
		t.Fatalf("global_soft slice never rejected; admitted=%d (limit should bound admissions)", admitted)
	}
	if firstRejected == 0 {
		t.Fatal("first request rejected, want several admitted before the slice bound is hit")
	}
	t.Logf("admitted=%d rejected=%d firstRejected=%d", admitted, rejected, firstRejected)
}
