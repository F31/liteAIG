package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/loadtest"
)

// TestGatewayLoadSmoke is the PR-level load gate: it drives the real gateway
// pipeline (auth, admission, routing, provider, accounting) against the mock
// provider and fails if throughput/latency/error SLOs regress badly.
func TestGatewayLoadSmoke(t *testing.T) {
	provider := newTestProviderServer(t)
	lite, err := NewLite(context.Background(), LiteOptions{
		DSN: testMemoryDSN(t, "gateway-load-smoke"),
		// This is a throughput smoke, not a rate-limit test.
		GatewayRPM: 1_000_000, GatewayBurst: 1_000_000,
	})
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

	report, err := loadtest.Run(context.Background(), loadtest.Config{
		BaseURL: gateway.URL, APIKey: setupResult.VirtualKey, Model: "default-chat",
		Duration: 2 * time.Second, Concurrency: 8, Prompts: 4,
		RPSFloor: 20, P99MSFloor: 500, MaxErrorRate: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.SLOMet() {
		t.Fatalf("load smoke missed SLO: rps=%.1f p99=%.1fms errors=%.2f%% violations=%v", report.RPS, report.LatencyP99MS, report.ErrorRate*100, report.SLOViolations)
	}
}
