package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/F31/liteAIG/internal/guardrail/benchmark"
)

func TestLiteEvidenceExportEndToEnd(t *testing.T) {
	provider := newTestProviderServer(t)
	lite, err := NewLite(context.Background(), LiteOptions{DSN: "file:" + filepath.Join(t.TempDir(), "evidence.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lite.Close(); err != nil {
			t.Error(err)
		}
	})
	admin := httptest.NewServer(lite.Handler())
	t.Cleanup(admin.Close)
	gateway := httptest.NewServer(lite.GatewayHandler())
	t.Cleanup(gateway.Close)
	status, body := doJSON(t, admin.URL+"/api/admin/setup", http.MethodPost, map[string]string{
		"username": "admin", "adminPassword": "password-123456", "tenantName": "Acme",
		"providerName": "Test Provider", "providerType": "openai",
		"providerEndpoint": providerEndpoint(provider), "providerSecret": testProviderKey,
		"selectedModel": "gpt-4o-mini",
	}, "", "")
	if status != http.StatusOK {
		t.Fatalf("setup status=%d body=%s", status, body)
	}
	var setup struct {
		VirtualKey string `json:"virtualKey"`
		TenantID   string `json:"tenantId"`
	}
	if err := json.Unmarshal([]byte(body), &setup); err != nil {
		t.Fatal(err)
	}
	if setup.VirtualKey == "" || setup.TenantID == "" {
		t.Fatal("missing setup key or tenant")
	}
	const prompt = "evidence-private-prompt-marker"
	status, body = doGateway(t, gateway.URL+"/v1/chat/completions", http.MethodPost, setup.VirtualKey, map[string]any{
		"model": "default-chat", "messages": []map[string]string{{"role": "user", "content": prompt}},
	})
	if status != http.StatusOK || !strings.Contains(body, "provider-echo") {
		t.Fatalf("chat status=%d body=%s", status, body)
	}
	status, body = doJSON(t, admin.URL+"/api/admin/evidence", http.MethodGet, nil, "", "")
	if status != http.StatusUnauthorized {
		t.Fatalf("anonymous evidence status=%d body=%s", status, body)
	}
	status, body, cookies := doJSONFull(t, admin.URL+"/api/admin/session", http.MethodPost, map[string]string{
		"username": "admin", "password": "password-123456",
	}, "")
	if status != http.StatusOK {
		t.Fatalf("login status=%d body=%s", status, body)
	}
	status, body = doJSON(t, admin.URL+"/api/admin/evidence", http.MethodGet, nil, cookies, "")
	if status != http.StatusOK {
		t.Fatalf("evidence status=%d body=%s", status, body)
	}
	var archive benchmark.Archive
	if err := json.Unmarshal([]byte(body), &archive); err != nil {
		t.Fatal(err)
	}
	if archive.TenantID != setup.TenantID || archive.ExportedAt.IsZero() || !archive.From.IsZero() || !archive.To.IsZero() || len(archive.Records) == 0 {
		t.Fatalf("invalid archive: %+v", archive)
	}
	for _, record := range archive.Records {
		if record.TenantID != setup.TenantID {
			t.Fatalf("cross-tenant record: %+v", record)
		}
		for key := range record.Detail {
			switch strings.ToLower(key) {
			case "prompt", "response", "body", "content", "secret", "password", "password_hash", "ciphertext", "authorization":
				t.Fatalf("sensitive detail key %q", key)
			}
		}
	}
	for _, excluded := range []string{prompt, "provider-echo", testProviderKey, setup.VirtualKey, "password-123456"} {
		if strings.Contains(body, excluded) {
			t.Fatalf("evidence leaked sensitive value %q", excluded)
		}
	}
}
