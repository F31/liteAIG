package mcp

import (
	"testing"

	"github.com/F31/liteAIG/internal/kernel/interaction"
)

func TestNormalizeToolCallAndDiscovery(t *testing.T) {
	request, err := Normalize([]byte(`{"jsonrpc":"2.0","id":"1","method":"tools/call","params":{"name":"invoice.read","arguments":{"id":"42"}}}`))
	if err != nil || request.Kind != interaction.RequestTool || request.Tool.Name != "invoice.read" {
		t.Fatalf("Normalize() = %+v, %v", request, err)
	}
	if Method(request) != "tools/call" {
		t.Fatalf("method = %q", Method(request))
	}
	discovery, err := Normalize([]byte(`{"jsonrpc":"2.0","id":"2","method":"server/discover","params":{}}`))
	if err != nil || discovery.Tool.Name != "server/discover" {
		t.Fatalf("discovery = %+v, %v", discovery, err)
	}
	task, err := Normalize([]byte(`{"jsonrpc":"2.0","id":"3","method":"tasks/create","params":{"title":"ship"}}`))
	if err != nil || task.Tool.Name != "tasks/create" || Method(task) != "tasks/create" {
		t.Fatalf("task = %+v, %v", task, err)
	}
	if _, err := Normalize([]byte(`{"jsonrpc":"2.0","method":"unknown"}`)); err == nil {
		t.Fatal("unsupported method accepted")
	}
}
