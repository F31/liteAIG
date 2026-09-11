package app

import (
	"bufio"
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

// setupLite initializes a fresh Lite plane and runs the Setup wizard.
// It returns the Lite, the test server, setup ids, and an authenticated
// (cookie + CSRF token) client pair.
func setupLite(t *testing.T) (*Lite, *httptest.Server, map[string]string, string, string) {
	t.Helper()
	provider := newTestProviderServer(t)
	lite, err := NewLite(context.Background(), LiteOptions{DSN: "file:lite-data-links?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(lite.Handler())
	t.Cleanup(func() { server.Close(); _ = lite.Close() })

	body := map[string]string{
		"username": "admin", "adminPassword": "password-123456", "tenantName": "Acme",
		"providerName": "Test Provider", "providerType": "openai", "providerEndpoint": providerEndpoint(provider),
		"providerSecret": testProviderKey, "selectedModel": "gpt-4o-mini",
	}
	status, out := doJSON(t, server.URL+"/api/admin/setup", "POST", body, "", "")
	if status != http.StatusOK {
		t.Fatalf("setup status = %d body = %s", status, out)
	}
	var result struct {
		TenantID  string `json:"tenantId"`
		ProjectID string `json:"projectId"`
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	status, loginBody, cookies := doJSONFull(t, server.URL+"/api/admin/session", "POST", map[string]string{
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
	return lite, server, map[string]string{"tenantId": result.TenantID, "projectId": result.ProjectID, "requestId": result.RequestID}, cookies, session.CSRFToken
}

func TestPlaygroundRunsProductionPipeline(t *testing.T) {
	_, server, setup, cookies, csrf := setupLite(t)
	_ = setup

	status, body := doJSON(t, server.URL+"/api/admin/playground", "POST", map[string]any{
		"model": "default-chat", "input": "hello", "stream": false,
	}, cookies, csrf)
	if status != http.StatusOK {
		t.Fatalf("playground status = %d body = %s", status, body)
	}
	var result struct {
		RequestID          string `json:"requestId"`
		Output             string `json:"output"`
		SelectedDeployment string `json:"selectedDeployment"`
		InputTokens        int64  `json:"inputTokens"`
		OutputTokens       int64  `json:"outputTokens"`
		LatencyMS          int64  `json:"latencyMS"`
	}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatal(err)
	}
	if result.RequestID == "" || result.SelectedDeployment == "" || result.InputTokens+result.OutputTokens == 0 {
		t.Fatalf("playground result incomplete: %+v", result)
	}

	// The same request_id is visible in Request Explorer with route evidence.
	status, body = doJSON(t, server.URL+"/api/admin/requests/"+result.RequestID, "GET", nil, cookies, "")
	if status != http.StatusOK {
		t.Fatalf("request detail status = %d body = %s", status, body)
	}
	for _, want := range []string{result.RequestID, "routeEvidence", "attempts", "success"} {
		if !strings.Contains(body, want) {
			t.Fatalf("request detail missing %q: %s", want, body)
		}
	}
}

func TestGovernanceSurfacesReturnRealData(t *testing.T) {
	_, server, setup, cookies, _ := setupLite(t)

	// Approvals: empty inbox renders as a real (empty) list, not errNotWired.
	status, body := doJSON(t, server.URL+"/api/admin/approvals", "GET", nil, cookies, "")
	if status != http.StatusOK || !strings.Contains(body, "[]") {
		t.Fatalf("approvals status = %d body = %s", status, body)
	}

	// Agent graph rebuilds from the ledger (Setup first call is a root).
	status, body = doJSON(t, server.URL+"/api/admin/agent-graph/"+setup["requestId"], "GET", nil, cookies, "")
	if status != http.StatusOK {
		t.Fatalf("agent-graph status = %d body = %s", status, body)
	}
	if !strings.Contains(body, "hops") {
		t.Fatalf("agent-graph body = %s", body)
	}

	// Federation view renders (empty relationships) without errNotWired.
	status, body = doJSON(t, server.URL+"/api/admin/federation", "GET", nil, cookies, "")
	if status != http.StatusOK {
		t.Fatalf("federation status = %d body = %s", status, body)
	}
}

func TestLiveTailStreamsRealEvents(t *testing.T) {
	_, server, _, cookies, csrf := setupLite(t)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/admin/live", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cookies != "" {
		req.Header.Set("Cookie", cookies)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("live status = %d", response.StatusCode)
	}

	// Trigger a playground request; its summary should arrive on the stream.
	go func() {
		_, _ = doJSONRaw(server.URL+"/api/admin/playground", "POST", map[string]any{
			"model": "default-chat", "input": "tail", "stream": false,
		}, cookies, csrf)
	}()

	lines := make(chan string, 16)
	go func() {
		scanner := bufio.NewScanner(response.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()

	for {
		select {
		case <-ctx.Done():
			t.Fatal("live tail did not deliver a request.summary event")
		case line, open := <-lines:
			if !open {
				t.Fatal("live stream closed before request.summary")
			}
			if strings.HasPrefix(line, "event: request.summary") {
				return // success: received a live event
			}
		}
	}
}

// doJSONRaw issues a JSON request without test helpers (safe inside goroutines).
func doJSONRaw(url, method string, payload any, cookies, csrf string) (int, string) {
	var reader io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return 0, err.Error()
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		return 0, err.Error()
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookies != "" {
		request.Header.Set("Cookie", cookies)
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, err.Error()
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(body)
}

func TestProjectsCreate(t *testing.T) {
	_, server, _, cookies, csrf := setupLite(t)
	status, body := doJSON(t, server.URL+"/api/admin/projects", "POST", map[string]any{
		"name": "Billing", "residencyEnforcement": "advisory", "allowedDataRegions": []string{"us", "eu"},
	}, cookies, csrf)
	if status != http.StatusOK {
		t.Fatalf("create project status = %d body = %s", status, body)
	}
	var created struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Name != "Billing" || created.Status != "active" {
		t.Fatalf("created project = %+v", created)
	}

	// It is now listed in the projects surface.
	status, body = doJSON(t, server.URL+"/api/admin/projects", "GET", nil, cookies, "")
	if status != http.StatusOK || !strings.Contains(body, "Billing") {
		t.Fatalf("list projects status = %d body = %s", status, body)
	}
}
