package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
)

func TestDiscoverInvokeAndAttribution(t *testing.T) {
	sink := &eventSink{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Mcp-Protocol-Version") != "2026-07-28" {
			t.Error("missing MCP version")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Mcp-Method") == "server/discover" {
			_, _ = w.Write([]byte(`{"result":{"tools":[{"name":"invoice.read","description":"read","inputSchema":{"type":"object"}}]}}`))
			return
		}
		if r.Header.Get("Mcp-Name") != "invoice.read" {
			t.Error("missing MCP tool name")
		}
		_, _ = w.Write([]byte(`{"result":{"content":"tool output"}}`))
	}))
	defer server.Close()
	connector, err := New(Config{BaseURL: server.URL, Timeout: time.Second}, server.Client(), sink)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := connector.Discover(context.Background())
	if err != nil || len(tools) != 1 || tools[0].Name != "invoice.read" {
		t.Fatalf("Discover()=%+v,%v", tools, err)
	}
	response, err := connector.Invoke(context.Background(), contracts.InvocationRequest{TenantID: "t", ProjectID: "p", RequestID: "r", SessionID: "s", TaskID: "task", AgentID: "agent", Request: &interaction.UnifiedRequest{Kind: interaction.RequestTool, Tool: &interaction.ToolPayload{Name: "invoice.read", Arguments: []byte(`{"id":"42"}`)}}})
	if err != nil || response.Response.ToolResult.Content != "tool output" || response.Response.ToolResult.Provenance.Trusted {
		t.Fatalf("Invoke()=%+v,%v", response, err)
	}
	if sink.event.Kind != "tool.call" || sink.event.Attributes["task_id"] != "task" || sink.event.Attributes["agent_id"] != "agent" {
		t.Fatalf("event=%+v", sink.event)
	}
}

func TestInvokeMCPTaskMethod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Mcp-Method") != "tasks/create" || r.Header.Get("Mcp-Name") != "tasks/create" {
			t.Fatalf("headers method=%q name=%q", r.Header.Get("Mcp-Method"), r.Header.Get("Mcp-Name"))
		}
		var body struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Method != "tasks/create" || body.Params["title"] != "ship" {
			t.Fatalf("body=%+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"taskId":"task-1","status":"created"}}`))
	}))
	defer server.Close()
	connector, err := New(Config{BaseURL: server.URL, Timeout: time.Second}, server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := connector.Invoke(context.Background(), contracts.InvocationRequest{RequestID: "r", Request: &interaction.UnifiedRequest{Kind: interaction.RequestTool, Metadata: map[string]string{"mcp_method": "tasks/create"}, Tool: &interaction.ToolPayload{Name: "tasks/create", Arguments: []byte(`{"title":"ship"}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Response.ToolResult.Content != `{"taskId":"task-1","status":"created"}` {
		t.Fatalf("content=%s", response.Response.ToolResult.Content)
	}
}

type eventSink struct{ event contracts.DomainEvent }

func (s *eventSink) Emit(_ context.Context, event contracts.DomainEvent) error {
	s.event = event
	return nil
}
