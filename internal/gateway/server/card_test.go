package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/F31/liteAIG/internal/access/protocol/a2a"
)

func TestAgentCardRoutePublishesConfiguredCard(t *testing.T) {
	core := &fakeCore{}
	card := &a2a.AgentCard{
		Name: "local-agent", URL: "https://local.example/agent",
		Description: "the local agent card", Version: "1.0.0",
		Skills: []string{"invoice.read"}, Capabilities: []string{"invoice.read"},
		Publisher: "local.example",
	}
	server := httptest.NewServer(New(core, Config{MaxBodyBytes: 1024, LocalAgentCard: card}).Handler())
	defer server.Close()

	resp, body := get(t, server.URL+"/.well-known/agent-card.json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=300" {
		t.Fatalf("cache-control = %q", cc)
	}
	for _, want := range []string{`"name":"local-agent"`, `"version":"1.0.0"`, `"url":"https://local.example/agent"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %s:\n%s", want, body)
		}
	}
}

func TestAgentCardRouteUnconfiguredReturns404(t *testing.T) {
	server := httptest.NewServer(New(&fakeCore{}, Config{MaxBodyBytes: 1024}).Handler())
	defer server.Close()

	resp, body := get(t, server.URL+"/.well-known/agent-card.json")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"code":"NOT_FOUND"`) {
		t.Fatalf("body missing NOT_FOUND code:\n%s", body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q", ct)
	}
}

func get(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(buf)
}
