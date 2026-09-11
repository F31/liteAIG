package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/F31/liteAIG/internal/access/protocol/a2a"
)

// TestLitePublishesLocalAgentCard verifies the LiteOptions.AgentCard is wired
// into the gateway's well-known publication route (an app-level integration of
// the server-level Config.LocalAgentCard).
func TestLitePublishesLocalAgentCard(t *testing.T) {
	lite, err := NewLite(context.Background(), LiteOptions{
		DSN: testMemoryDSN(t, "agent-card-pub"),
		AgentCard: &a2a.AgentCard{
			Name: "local-agent", URL: "https://local.example/agent",
			Description: "published by lite", Version: "1.0.0",
			Publisher: "local.example",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer lite.Close()

	gateway := httptest.NewServer(lite.GatewayHandler())
	defer gateway.Close()

	resp, err := http.Get(gateway.URL + "/.well-known/agent-card.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body = %s", resp.StatusCode, body)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=300" {
		t.Fatalf("cache-control = %q", cc)
	}
	if !strings.Contains(string(body), `"name":"local-agent"`) || !strings.Contains(string(body), `"version":"1.0.0"`) {
		t.Fatalf("published card missing fields: %s", body)
	}
}

func TestLiteWithoutAgentCardReturnsNotFound(t *testing.T) {
	lite, err := NewLite(context.Background(), LiteOptions{DSN: testMemoryDSN(t, "agent-card-none")})
	if err != nil {
		t.Fatal(err)
	}
	defer lite.Close()

	gateway := httptest.NewServer(lite.GatewayHandler())
	defer gateway.Close()

	resp, err := http.Get(gateway.URL + "/.well-known/agent-card.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d body = %s, want 404", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"code":"NOT_FOUND"`) {
		t.Fatalf("404 body missing NOT_FOUND code: %s", body)
	}
}
