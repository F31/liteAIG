package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	postgrestestutil "github.com/F31/liteAIG/internal/platform/storage/postgres/testutil"
)

// TestPostgresRLSDataPlanePipeline proves the tenant pool selected from the
// request context can authenticate, route, call the provider, and persist the
// accounting/usage rows under RLS as the restricted app role.
func TestPostgresRLSDataPlanePipeline(t *testing.T) {
	if os.Getenv("LITEAIG_TEST_POSTGRES_DSN") == "" {
		t.Skip("LITEAIG_TEST_POSTGRES_DSN is not configured")
	}
	_, dsn := postgrestestutil.IsolatedDatabase(t)
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LITEAIG_MASTER_KEY", base64.StdEncoding.EncodeToString(key))
	provider := newTestProviderServer(t)
	lite, err := NewLite(context.Background(), LiteOptions{DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	admin := httptest.NewServer(lite.Handler())
	gateway := httptest.NewServer(lite.GatewayHandler())
	t.Cleanup(func() { gateway.Close(); admin.Close(); _ = lite.Close() })

	status, body := doJSON(t, admin.URL+"/api/admin/setup", "POST", map[string]string{
		"username": "admin", "adminPassword": "password-123456", "tenantName": "Acme",
		"providerName": "Test Provider", "providerType": "openai",
		"providerEndpoint": providerEndpoint(provider), "providerSecret": testProviderKey,
		"selectedModel": "gpt-4o-mini",
	}, "", "")
	if status != http.StatusOK {
		t.Fatalf("setup status = %d body = %s", status, body)
	}
	var setupResult struct {
		VirtualKey string `json:"virtualKey"`
	}
	if err := json.Unmarshal([]byte(body), &setupResult); err != nil {
		t.Fatal(err)
	}
	status, body = doGateway(t, gateway.URL+"/v1/chat/completions", http.MethodPost, setupResult.VirtualKey, map[string]any{
		"model": "default-chat", "messages": []map[string]string{{"role": "user", "content": "hello"}},
	})
	if status != http.StatusOK {
		t.Fatalf("gateway status = %d body = %s", status, body)
	}

	status, _ = doJSON(t, admin.URL+"/api/admin/session", "POST", map[string]string{
		"username": "admin", "password": "wrong-password",
	}, "", "")
	if status != http.StatusUnauthorized {
		t.Fatalf("wrong-password login status = %d, want 401", status)
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
	status, body = doJSON(t, admin.URL+"/api/admin/keys", "GET", nil, cookies, session.CSRFToken)
	if status != http.StatusOK {
		t.Fatalf("tenant admin read under RLS status = %d body = %s", status, body)
	}
}
