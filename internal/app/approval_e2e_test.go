package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestRequireApprovalBlocksUntilHumanDecision proves the closed loop for
// high-risk actions on the data plane: a tool whose published policy sets
// require_approval is refused with 409 REQUIRE_APPROVAL and an approval
// request is opened; once an operator approves it in the admin API, the same
// call succeeds. The accounting ledger records the blocked attempt.
func TestRequireApprovalBlocksUntilHumanDecision(t *testing.T) {
	provider := newTestProviderServer(t)
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": "paid"}})
	}))
	defer mcp.Close()

	notifications := make(chan string, 8)
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event struct {
			Kind string `json:"kind"`
		}
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Error(err)
		}
		notifications <- event.Kind
		w.WriteHeader(http.StatusServiceUnavailable) // Delivery failure must not affect approvals or gateway results.
	}))
	defer webhook.Close()
	lite, err := NewLite(context.Background(), LiteOptions{DSN: testMemoryDSN(t, "approval-e2e"), WebhookURL: webhook.URL})
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
		TenantID   string `json:"tenantId"`
		ProjectID  string `json:"projectId"`
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

	// Publish a config with one tool gated by require_approval.
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
	document["mcp_servers"] = []map[string]string{{"id": "mcp-pay", "tenant_id": setupResult.TenantID, "url": mcp.URL, "status": "active"}}
	document["tools"] = []map[string]any{
		{"id": "tool-pay", "tenant_id": setupResult.TenantID, "server_id": "mcp-pay", "name": "payment.execute", "status": "active", "schema": map[string]string{"type": "object"}},
	}
	document["tool_policies"] = []map[string]any{
		{"tool_id": "tool-pay", "tenant_id": setupResult.TenantID, "project_id": setupResult.ProjectID, "allowed": true, "require_approval": true},
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

	callTool := func() (int, string) {
		payload, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "tools/call",
			"params": map[string]any{"name": "payment.execute", "arguments": map[string]any{"amount": 10}},
		})
		request, err := http.NewRequest(http.MethodPost, gateway.URL+"/mcp", bytes.NewReader(payload))
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
		out, _ := io.ReadAll(response.Body)
		return response.StatusCode, string(out)
	}

	// 1. Ungated: the call is refused before reaching the upstream.
	if status, body := callTool(); status != http.StatusConflict || !strings.Contains(body, "REQUIRE_APPROVAL") {
		t.Fatalf("gated tool call status = %d body = %s, want 409 REQUIRE_APPROVAL", status, body)
	}

	// 2. The pending request appears in the operator inbox.
	status, body = doJSON(t, admin.URL+"/api/admin/approvals", "GET", nil, cookies, "")
	if status != http.StatusOK {
		t.Fatalf("approvals status = %d body = %s", status, body)
	}
	var inbox []struct {
		ID     string `json:"id"`
		Target string `json:"target"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(body), &inbox); err != nil {
		t.Fatal(err)
	}
	if len(inbox) != 1 || inbox[0].Status != "pending" || inbox[0].Target != "tool-pay" {
		t.Fatalf("approval inbox = %s, want one pending request for tool-pay", body)
	}

	// 3. A second blocked attempt must not duplicate the pending request.
	if status, _ := callTool(); status != http.StatusConflict {
		t.Fatalf("second gated call status = %d, want 409", status)
	}
	status, body = doJSON(t, admin.URL+"/api/admin/approvals", "GET", nil, cookies, "")
	if status != http.StatusOK {
		t.Fatalf("approvals status = %d body = %s", status, body)
	}
	if err := json.Unmarshal([]byte(body), &inbox); err != nil {
		t.Fatal(err)
	}
	if len(inbox) != 1 {
		t.Fatalf("approval inbox = %s, want exactly one request (idempotent)", body)
	}

	// 4. Operator approves; the same action now passes.
	status, body = doJSON(t, admin.URL+"/api/admin/approvals/"+inbox[0].ID+"/action", "POST", map[string]string{"decision": "approve"}, cookies, session.CSRFToken)
	if status != http.StatusOK {
		t.Fatalf("approve status = %d body = %s", status, body)
	}
	if status, body := callTool(); status != http.StatusOK || !strings.Contains(body, "paid") {
		t.Fatalf("approved tool call status = %d body = %s, want 200 paid", status, body)
	}

	// 5. The blocked attempt is accounted for with the approval outcome.
	status, body = doJSON(t, admin.URL+"/api/admin/requests", "GET", nil, cookies, "")
	if status != http.StatusOK {
		t.Fatalf("requests status = %d body = %s", status, body)
	}
	var records []map[string]any
	if err := json.Unmarshal([]byte(body), &records); err != nil {
		t.Fatal(err)
	}
	blocked, succeeded := false, false
	for _, record := range records {
		if record["source"] != "mcp" {
			continue
		}
		if record["outcome"] == "REQUIRE_APPROVAL" {
			blocked = true
		}
		if record["outcome"] == "success" {
			succeeded = true
		}
	}
	if !blocked || !succeeded {
		t.Fatalf("accounting outcomes (blocked=%v success=%v) missing: %s", blocked, succeeded, body)
	}
	for _, want := range []string{"approval.created", "approval.approved"} {
		select {
		case got := <-notifications:
			if got != want {
				t.Fatalf("webhook kind = %q, want %q", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("missing %s webhook", want)
		}
	}
}
