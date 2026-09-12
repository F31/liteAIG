package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestLiteManagementPlaneEndToEnd(t *testing.T) {
	provider := newTestProviderServer(t)
	lite, err := NewLite(context.Background(), LiteOptions{
		DSN: testMemoryDSN(t, "lite-e2e"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer lite.Close()

	server := httptest.NewServer(lite.Handler())
	defer server.Close()

	// 1. Setup wizard closure against a real OpenAI-compatible endpoint.
	setupBody := map[string]string{
		"username":         "admin",
		"adminPassword":    "password-123456",
		"tenantName":       "Acme",
		"providerName":     "Test Provider",
		"providerType":     "openai",
		"providerEndpoint": providerEndpoint(provider),
		"providerSecret":   testProviderKey,
		"selectedModel":    "gpt-4o-mini",
	}
	status, body := doJSON(t, server.URL+"/api/admin/setup", "POST", setupBody, "", "")
	if status != http.StatusOK {
		t.Fatalf("setup status = %d body = %s", status, body)
	}
	var setupResult struct {
		VirtualKey string `json:"virtualKey"`
		SDKExample string `json:"sdkExample"`
		RequestID  string `json:"requestId"`
		TenantID   string `json:"tenantId"`
		ProjectID  string `json:"projectId"`
	}
	if err := json.Unmarshal([]byte(body), &setupResult); err != nil {
		t.Fatal(err)
	}
	if setupResult.VirtualKey == "" || setupResult.RequestID == "" || setupResult.TenantID == "" || setupResult.ProjectID == "" {
		t.Fatalf("incomplete setup result: %+v", setupResult)
	}

	// Setup is idempotent-guarded: a second call fails.
	status, body = doJSON(t, server.URL+"/api/admin/setup", "POST", setupBody, "", "")
	if status == http.StatusOK {
		t.Fatalf("second setup unexpectedly succeeded: %s", body)
	}

	// 2. Local login establishes a session and CSRF.
	status, body, cookies := doJSONFull(t, server.URL+"/api/admin/session", "POST", map[string]string{
		"username": "admin", "password": "password-123456",
	}, "")
	if status != http.StatusOK {
		t.Fatalf("login status = %d body = %s", status, body)
	}
	var sessionResult struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal([]byte(body), &sessionResult); err != nil {
		t.Fatal(err)
	}
	if sessionResult.CSRFToken == "" {
		t.Fatal("missing csrfToken")
	}

	// 3. Authenticated /me resolves the tenant scope.
	status, body = doJSON(t, server.URL+"/api/admin/me", "GET", nil, cookies, "")
	if status != http.StatusOK {
		t.Fatalf("me status = %d body = %s", status, body)
	}
	if !strings.Contains(body, setupResult.TenantID) {
		t.Fatalf("me body = %s, want tenant %s", body, setupResult.TenantID)
	}

	// 4. Runtime reflects the configured tenant.
	status, body = doJSON(t, server.URL+"/api/admin/runtime", "GET", nil, cookies, "")
	if status != http.StatusOK {
		t.Fatalf("runtime status = %d body = %s", status, body)
	}
	for _, want := range []string{"providers", "deployments", "logicalModels", "routes"} {
		if !strings.Contains(body, want) {
			t.Fatalf("runtime body missing %q: %s", want, body)
		}
	}

	status, body = doJSON(t, server.URL+"/api/admin/keys", "POST", map[string]any{
		"Name":           "console-key",
		"ProjectID":      setupResult.ProjectID,
		"ModelAllowlist": []string{"default-chat"},
		"IPAllowlist":    []string{},
	}, cookies, sessionResult.CSRFToken)
	if status != http.StatusOK {
		t.Fatalf("create key status = %d body = %s", status, body)
	}
	var createKeyResult struct {
		Key    string `json:"Key"`
		Record struct {
			ID string `json:"ID"`
		} `json:"Record"`
	}
	if err := json.Unmarshal([]byte(body), &createKeyResult); err != nil {
		t.Fatal(err)
	}
	if len(createKeyResult.Key) != 108 {
		t.Fatalf("key length = %d, want compact 108: %s", len(createKeyResult.Key), createKeyResult.Key)
	}

	// A freshly created key must work on the data plane immediately, without a
	// config re-publish.
	gateway := httptest.NewServer(lite.GatewayHandler())
	defer gateway.Close()
	status, body = doGateway(t, gateway.URL+"/v1/chat/completions", "POST", createKeyResult.Key, map[string]any{
		"model":    "default-chat",
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
	})
	if status != http.StatusOK {
		t.Fatalf("gateway with fresh key status = %d body = %s", status, body)
	}

	// The key must be listed in the admin view and re-revealable on demand.
	status, body = doJSON(t, server.URL+"/api/admin/keys", "GET", nil, cookies, "")
	if status != http.StatusOK {
		t.Fatalf("list keys status = %d body = %s", status, body)
	}
	if !strings.Contains(body, `"revealable":true`) {
		t.Fatalf("list keys body missing revealable key: %s", body)
	}
	status, body = doJSON(t, server.URL+"/api/admin/keys/"+createKeyResult.Record.ID, "GET", nil, cookies, "")
	if status != http.StatusOK {
		t.Fatalf("reveal key status = %d body = %s", status, body)
	}
	if !strings.Contains(body, createKeyResult.Key) {
		t.Fatalf("reveal key body mismatch: %s", body)
	}

	// A revoked key must stop working immediately.
	status, _ = doJSONFullReauth(t, server.URL+"/api/admin/keys/"+createKeyResult.Record.ID+"/revoke", "POST", nil, cookies, sessionResult.CSRFToken, "password-123456")
	if status != http.StatusOK {
		t.Fatalf("revoke key status = %d", status)
	}
	status, body = doGateway(t, gateway.URL+"/v1/chat/completions", "POST", createKeyResult.Key, map[string]any{
		"model":    "default-chat",
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("gateway with revoked key status = %d body = %s, want 401", status, body)
	}

	// 5. First-call request record is visible in Request Explorer and carries
	// real provider usage + derived cost.
	status, body = doJSON(t, server.URL+"/api/admin/requests", "GET", nil, cookies, "")
	if status != http.StatusOK {
		t.Fatalf("requests status = %d body = %s", status, body)
	}
	if !strings.Contains(body, setupResult.RequestID) {
		t.Fatalf("requests body missing first request %q: %s", setupResult.RequestID, body)
	}
	type recordView struct {
		RequestID        string   `json:"requestId"`
		InputTokens      int64    `json:"inputTokens"`
		OutputTokens     int64    `json:"outputTokens"`
		ProviderCost     *float64 `json:"providerCost"`
		ProviderCurrency string   `json:"providerCurrency"`
	}
	var records []recordView
	if err := json.Unmarshal([]byte(body), &records); err != nil {
		t.Fatalf("decode requests: %v body=%s", err, body)
	}
	var first *recordView
	for i := range records {
		if records[i].RequestID == setupResult.RequestID {
			first = &records[i]
		}
	}
	if first == nil {
		t.Fatalf("first request %s not in records: %s", setupResult.RequestID, body)
	}
	if first.InputTokens != 3 || first.OutputTokens != 5 {
		t.Fatalf("usage = %d/%d, want the 3/5 reported by the provider", first.InputTokens, first.OutputTokens)
	}
	if first.ProviderCost == nil || *first.ProviderCost <= 0 || first.ProviderCurrency != "USD" {
		t.Fatalf("provider cost missing: %+v", first)
	}

	// 6. Streaming playground: live SSE tokens from the real provider, then a
	// final done event carrying usage and cost.
	sse, err := doSSE(t, server.URL+"/api/admin/playground", map[string]any{
		"model": "default-chat", "input": "hello", "stream": true,
	}, cookies, sessionResult.CSRFToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(sse.Tokens) < 2 || sse.Tokens[0].Delta != "stream-" || sse.Tokens[1].Delta != "ok" || !sse.Tokens[1].Final {
		t.Fatalf("token events = %+v, want stream- then ok(final)", sse.Tokens)
	}
	if sse.Tokens[1].StopReason != "stop" {
		t.Fatalf("token stopReason = %q, want stop", sse.Tokens[1].StopReason)
	}
	if sse.Done.Output != "stream-ok" {
		t.Fatalf("done output = %q, want stream-ok", sse.Done.Output)
	}
	if sse.Done.InputTokens != 3 || sse.Done.OutputTokens != 5 {
		t.Fatalf("done usage = %d/%d, want 3/5", sse.Done.InputTokens, sse.Done.OutputTokens)
	}
	if sse.Done.Cost <= 0 {
		t.Fatalf("done cost = %v, want > 0", sse.Done.Cost)
	}

	// 7. Console SPA is served without capturing API paths.
	resp, err := http.Get(server.URL + "/dashboard")
	if err != nil {
		t.Fatal(err)
	}
	content, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(content), "<div id=\"root\"></div>") {
		t.Fatalf("console status = %d body = %s", resp.StatusCode, string(content))
	}
}

func TestDataPlaneGatewayEndToEnd(t *testing.T) {
	provider := newTestProviderServer(t)
	lite, err := NewLite(context.Background(), LiteOptions{DSN: testMemoryDSN(t, "gateway-e2e")})
	if err != nil {
		t.Fatal(err)
	}
	defer lite.Close()
	if lite.GatewayHandler() == nil {
		t.Fatal("gateway handler not wired")
	}
	gateway := httptest.NewServer(lite.GatewayHandler())
	defer gateway.Close()
	admin := httptest.NewServer(lite.Handler())
	defer admin.Close()

	// Setup creates the tenant, publishes config, and issues a virtual key.
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
	}
	if err := json.Unmarshal([]byte(body), &setupResult); err != nil {
		t.Fatal(err)
	}
	if setupResult.VirtualKey == "" {
		t.Fatalf("setup did not return a virtual key: %s", body)
	}
	key := setupResult.VirtualKey

	// 1. /v1/models lists the visible logical model.
	status, body = doGateway(t, gateway.URL+"/v1/models", http.MethodGet, key, nil)
	if status != http.StatusOK {
		t.Fatalf("models status = %d body = %s", status, body)
	}
	if !strings.Contains(body, "default-chat") {
		t.Fatalf("models body missing default-chat: %s", body)
	}

	// 2. /v1/chat/completions (blocking) returns the provider echo + usage.
	status, body = doGateway(t, gateway.URL+"/v1/chat/completions", http.MethodPost, key, map[string]any{
		"model": "default-chat", "messages": []map[string]string{{"role": "user", "content": "hello"}},
	})
	if status != http.StatusOK {
		t.Fatalf("chat status = %d body = %s", status, body)
	}
	var chat struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(body), &chat); err != nil {
		t.Fatalf("decode chat: %v body=%s", err, body)
	}
	if chat.Model != "default-chat" || len(chat.Choices) == 0 || chat.Choices[0].Message.Content != "provider-echo" {
		t.Fatalf("chat = %+v, want model default-chat + provider-echo", chat)
	}
	if chat.Usage.PromptTokens != 3 || chat.Usage.CompletionTokens != 5 {
		t.Fatalf("chat usage = %+v, want 3/5", chat.Usage)
	}

	// 3. /v1/chat/completions (streaming) returns OpenAI SSE chunks + [DONE].
	stream, err := doGatewayStream(t, gateway.URL+"/v1/chat/completions", key, map[string]any{
		"model": "default-chat", "stream": true, "messages": []map[string]string{{"role": "user", "content": "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stream, "data: [DONE]\n\n") {
		t.Fatalf("stream missing terminal [DONE]: %q", stream)
	}
	if !strings.Contains(stream, "stream-") || !strings.Contains(stream, "ok") {
		t.Fatalf("stream missing deltas: %q", stream)
	}

	// 4. A missing/invalid credential is rejected before any pipeline work.
	status, body = doGateway(t, gateway.URL+"/v1/chat/completions", http.MethodPost, "sk-lia-v1:bad:bad", map[string]any{
		"model": "default-chat", "messages": []map[string]string{{"role": "user", "content": "hello"}},
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("invalid key status = %d body = %s, want 401", status, body)
	}

	// 5. Data-plane requests are recorded with source "gateway".
	_, body, cookies := doJSONFull(t, admin.URL+"/api/admin/session", "POST", map[string]string{
		"username": "admin", "password": "password-123456",
	}, "")
	status, body = doJSON(t, admin.URL+"/api/admin/requests", "GET", nil, cookies, "")
	if status != http.StatusOK {
		t.Fatalf("requests status = %d body = %s", status, body)
	}
	if !strings.Contains(body, `"source":"gateway"`) {
		t.Fatalf("requests missing gateway-sourced records: %s", body)
	}
}

// TestLiteRestartReactivatesRuntime guards the P0 restart defect: tenant
// runtime snapshots are in-memory, so a process restart used to drop them and
// the data plane rejected all traffic (401) until the next manual publish.
// The startup reconcile must restore snapshots from the published versions so
// a restart is transparent to data-plane clients.
func TestLiteRestartReactivatesRuntime(t *testing.T) {
	provider := newTestProviderServer(t)
	dsn := "file:" + filepath.Join(t.TempDir(), "restart.db")

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

	// First boot: wizard creates the tenant, publishes config v1, issues a key.
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
	key := setupResult.VirtualKey

	chatBody := map[string]any{
		"model": "default-chat", "messages": []map[string]string{{"role": "user", "content": "hello"}},
	}
	status, body = doGateway(t, gateway.URL+"/v1/chat/completions", http.MethodPost, key, chatBody)
	if status != http.StatusOK {
		t.Fatalf("pre-restart chat status = %d body = %s, want 200", status, body)
	}
	if !lite.HasActiveRuntime() {
		t.Fatal("runtime not active before restart")
	}

	// Simulate a process restart: tear down the servers and the Lite instance,
	// then compose a fresh one against the same on-database.
	gateway.Close()
	admin.Close()
	if err := lite.Close(); err != nil {
		t.Fatal(err)
	}

	lite = boot()
	defer lite.Close()
	if !lite.HasActiveRuntime() {
		t.Fatal("runtime was not restored after restart")
	}
	admin = httptest.NewServer(lite.Handler())
	defer admin.Close()
	gateway = httptest.NewServer(lite.GatewayHandler())
	defer gateway.Close()

	// The same key must work again with no manual republish.
	status, body = doGateway(t, gateway.URL+"/v1/chat/completions", http.MethodPost, key, chatBody)
	if status != http.StatusOK {
		t.Fatalf("post-restart chat status = %d body = %s, want 200", status, body)
	}
	var chat struct {
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(body), &chat); err != nil {
		t.Fatalf("decode post-restart chat: %v body=%s", err, body)
	}
	if chat.Usage.PromptTokens != 3 || chat.Usage.CompletionTokens != 5 {
		t.Fatalf("post-restart usage = %+v, want 3/5", chat.Usage)
	}
}

// doGateway performs an authenticated data-plane request and returns the
// status code and response body.
func doGateway(t *testing.T, url, method, key string, payload any) (int, string) {
	t.Helper()
	var reader *bytes.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(body)
}

// doGatewayStream performs a streaming data-plane request and returns the raw
// SSE body.
func doGatewayStream(t *testing.T, url, key string, payload any) (string, error) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		return "", fmt.Errorf("status = %d body = %s", response.StatusCode, body)
	}
	raw, err := io.ReadAll(response.Body)
	return string(raw), err
}

type sseToken struct {
	Delta      string `json:"delta"`
	StopReason string `json:"stopReason"`
	Final      bool   `json:"final"`
}
type sseDone struct {
	Output       string  `json:"output"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	Cost         float64 `json:"cost"`
}
type sseResult struct {
	Tokens []sseToken
	Done   sseDone
}

// doSSE performs a POST that streams SSE and parses token/done events.
func doSSE(t *testing.T, url string, payload any, cookies, csrf string) (sseResult, error) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		return sseResult{}, err
	}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return sseResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	if cookies != "" {
		request.Header.Set("Cookie", cookies)
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return sseResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		return sseResult{}, fmt.Errorf("status = %d body = %s", response.StatusCode, body)
	}
	var result sseResult
	var current string
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			current = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: ") && current == "token":
			var token sseToken
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &token); err != nil {
				return sseResult{}, err
			}
			result.Tokens = append(result.Tokens, token)
		case strings.HasPrefix(line, "data: ") && current == "done":
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &result.Done); err != nil {
				return sseResult{}, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return sseResult{}, err
	}
	return result, nil
}

func doJSON(t *testing.T, url, method string, payload any, cookies, csrf string) (int, string) {
	t.Helper()
	status, body, _ := doJSONFull(t, url, method, payload, cookies, csrf)
	return status, body
}

func doJSONFullReauth(t *testing.T, url, method string, payload any, cookies, csrf, reauth string) (int, string) {
	t.Helper()
	var reader io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Cookie", cookies)
	request.Header.Set("X-CSRF-Token", csrf)
	request.Header.Set("X-Reauth-Token", reauth)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(body)
}

func doJSONFull(t *testing.T, url, method string, payload any, cookies string, csrf ...string) (int, string, string) {
	t.Helper()
	var reader io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookies != "" {
		request.Header.Set("Cookie", cookies)
	}
	if len(csrf) > 0 && csrf[0] != "" {
		request.Header.Set("X-CSRF-Token", csrf[0])
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	var setCookies []string
	for _, cookie := range response.Cookies() {
		setCookies = append(setCookies, cookie.Name+"="+cookie.Value)
	}
	return response.StatusCode, string(body), strings.Join(setCookies, "; ")
}
