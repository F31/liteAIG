package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/F31/liteAIG/internal/finops/budget"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/platform/id"
	"github.com/F31/liteAIG/internal/policy/rate"
)

// TestAdmitToolReservesBudgetAndFailureReleases proves MCP/A2A traffic goes
// through the same admission (rate + budget reservation) as model traffic and
// that a failed interaction releases its reservation.
func TestAdmitToolReservesBudgetAndFailureReleases(t *testing.T) {
	p := &litePipeline{
		budget:     newBudgetEnforcer(budget.New(systemClock{})),
		limiter:    rate.New(systemClock{}),
		accounting: &recordingAccountingRepo{},
		ids:        id.NewGenerator(nil),
		clock:      systemClock{},
	}
	snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "t1", TenantRef: "t1", Status: "active", Version: 1,
		BudgetPolicies: []runtime.BudgetPolicy{{ID: "b1", TenantID: "t1", WindowHours: 1, TokenLimit: 10, Mode: "hard"}},
	})
	request := &kernel.RequestContext{
		RequestID:  "req-1",
		ReceivedAt: p.clock.Now(),
		Snapshot:   snapshot,
		Interaction: &interaction.Context{
			Kind: interaction.KindTool, TenantID: "t1", ProjectID: "p1",
		},
		Request: &interaction.UnifiedRequest{Kind: interaction.RequestTool, Tool: &interaction.ToolPayload{Name: "ping", Arguments: []byte(`{"x":1}`)}},
		Key:     runtime.APIKey{PublicID: "key-1", ProjectID: "p1"},
		Source:  "gateway",
	}
	if err := p.admitTool(request); err != nil {
		t.Fatalf("admitTool() = %v", err)
	}
	if request.ReservationID == "" || request.BudgetPolicyID != "b1" {
		t.Fatalf("no budget reservation recorded: %+v", request)
	}
	if reserved, _ := p.budget.Usage("b1"); reserved == 0 {
		t.Fatal("budget window has no reserved tokens")
	}
	// A failed interaction must release the reservation (the finalizer does).
	if err := p.finalizeTool(context.Background(), request, "tool-1", nil, context.DeadlineExceeded); err != nil {
		t.Fatalf("finalizeTool() = %v", err)
	}
	if reserved, consumed := p.budget.Usage("b1"); reserved != 0 || consumed != 0 {
		t.Fatalf("budget not released after failure: reserved=%d consumed=%d", reserved, consumed)
	}
}

// TestMCPAndA2ATrafficIsGoverned proves the closed loop for non-model data
// plane traffic: a retryable upstream failure is retried once, a persistent
// failure returns 502, and both outcomes land in accounting.
func TestMCPAndA2ATrafficIsGoverned(t *testing.T) {
	provider := newTestProviderServer(t)

	var flakyCalls int32
	flakyMCP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Mcp-Method") == "tasks/create" {
			_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"taskId": "task-1", "status": "created"}})
			return
		}
		if atomic.AddInt32(&flakyCalls, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": "pong"}})
	}))
	defer flakyMCP.Close()
	badMCP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer badMCP.Close()

	lite, err := NewLite(context.Background(), LiteOptions{DSN: testMemoryDSN(t, "mcp-governance")})
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

	// Publish a config that adds two MCP servers (one flaky, one broken) and
	// one tool on each, with an explicit tool policy.
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
	document["mcp_servers"] = []map[string]string{
		{"id": "mcp-flaky", "tenant_id": setupResult.TenantID, "url": flakyMCP.URL, "status": "active"},
		{"id": "mcp-bad", "tenant_id": setupResult.TenantID, "url": badMCP.URL, "status": "active"},
	}
	document["tools"] = []map[string]any{
		{"id": "tool-ping", "tenant_id": setupResult.TenantID, "server_id": "mcp-flaky", "name": "ping", "status": "active", "schema": map[string]string{"type": "object"}},
		{"id": "tool-task-create", "tenant_id": setupResult.TenantID, "server_id": "mcp-flaky", "name": "tasks/create", "status": "active", "schema": map[string]string{"type": "object"}},
		{"id": "tool-boom", "tenant_id": setupResult.TenantID, "server_id": "mcp-bad", "name": "boom", "status": "active", "schema": map[string]string{"type": "object"}},
	}
	document["tool_policies"] = []map[string]any{
		{"tool_id": "tool-ping", "tenant_id": setupResult.TenantID, "project_id": setupResult.ProjectID, "allowed": true},
		{"tool_id": "tool-task-create", "tenant_id": setupResult.TenantID, "project_id": setupResult.ProjectID, "allowed": true},
		{"tool_id": "tool-boom", "tenant_id": setupResult.TenantID, "project_id": setupResult.ProjectID, "allowed": true},
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

	callTool := func(name string) (int, string) {
		payload, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "tools/call",
			"params": map[string]any{"name": name, "arguments": map[string]any{"x": 1}},
		})
		if err != nil {
			t.Fatal(err)
		}
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

	// Flaky upstream: first attempt 500, retry succeeds.
	status, body = callTool("ping")
	if status != http.StatusOK {
		t.Fatalf("flaky tool call status = %d body = %s, want 200", status, body)
	}
	if !strings.Contains(body, "pong") {
		t.Fatalf("flaky tool response = %s, want pong", body)
	}
	if calls := atomic.LoadInt32(&flakyCalls); calls != 2 {
		t.Fatalf("flaky upstream calls = %d, want 2 (one retry)", calls)
	}

	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": "task", "method": "tasks/create",
		"params": map[string]any{"title": "ship"},
	})
	if err != nil {
		t.Fatal(err)
	}
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
	out, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(out), `"taskId":"task-1"`) || strings.Contains(string(out), `"content"`) {
		t.Fatalf("task call status = %d body = %s, want raw MCP result", response.StatusCode, out)
	}

	// Persistent upstream failure: 502 to the caller.
	if status, body := callTool("boom"); status != http.StatusBadGateway {
		t.Fatalf("bad tool call status = %d body = %s, want 502", status, body)
	}

	// Both outcomes are accounted for.
	status, body = doJSON(t, admin.URL+"/api/admin/requests", "GET", nil, cookies, "")
	if status != http.StatusOK {
		t.Fatalf("requests status = %d body = %s", status, body)
	}
	var records []map[string]any
	if err := json.Unmarshal([]byte(body), &records); err != nil {
		t.Fatal(err)
	}
	seenSuccess, seenFailure := false, false
	for _, record := range records {
		if record["source"] != "mcp" {
			continue
		}
		if record["outcome"] == "success" {
			seenSuccess = true
		}
		if record["outcome"] == "MCP_UPSTREAM_ERROR" {
			seenFailure = true
		}
	}
	if !seenSuccess || !seenFailure {
		t.Fatalf("accounting missing MCP outcomes (success=%v failure=%v): %s", seenSuccess, seenFailure, body)
	}
}
