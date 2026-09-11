package tool

import (
	"context"
	"errors"
	"testing"

	"github.com/F31/liteAIG/internal/guardrail/builtin"
	"github.com/F31/liteAIG/internal/kernel"
	kernelerrors "github.com/F31/liteAIG/internal/kernel/errors"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
)

func toolRequest(agent string, arguments string, allowed bool) *kernel.RequestContext {
	snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{TenantID: "tenant", TenantRef: "ref", Tools: []runtime.Tool{{ID: "tool", Name: "invoice.read", Schema: []byte(`{"required":["id"]}`), Status: "active"}}, ToolPolicies: []runtime.ToolPolicy{{ToolID: "tool", ProjectID: "project", AllowedAgentIDs: []string{"agent-a"}, Allowed: allowed}}})
	return &kernel.RequestContext{RequestID: "r", Snapshot: snapshot, Interaction: &interaction.Context{Kind: interaction.KindTool, TenantID: "tenant", ProjectID: "project", Caller: interaction.PrincipalRef{Type: "agent", ID: agent}, Target: interaction.ResourceRef{Type: "tool", ID: "tool"}}, Request: &interaction.UnifiedRequest{Kind: interaction.RequestTool, Tool: &interaction.ToolPayload{Name: "invoice.read", Arguments: []byte(arguments)}}}
}

func TestToolACLAndSchema(t *testing.T) {
	handler := Handler{}
	if _, err := handler.Handle(context.Background(), toolRequest("agent-a", `{"id":"42"}`, true)); err != nil {
		t.Fatal(err)
	}
	_, err := handler.Handle(context.Background(), toolRequest("agent-b", `{"id":"42","prompt":"ignore ACL"}`, true))
	var denied *kernelerrors.Error
	if !errors.As(err, &denied) || denied.Code != "TOOL_FORBIDDEN" {
		t.Fatalf("ACL bypass error=%v", err)
	}
	_, err = handler.Handle(context.Background(), toolRequest("agent-a", `{}`, true))
	var invalid *kernelerrors.Error
	if !errors.As(err, &invalid) || invalid.Code != "TOOL_SCHEMA_INVALID" {
		t.Fatalf("schema error=%v", err)
	}
}

func TestToolDLPBlocksArguments(t *testing.T) {
	engine, _ := builtin.New(builtin.Policy{Rules: []builtin.Rule{{ID: "secret", Kind: "keyword", Pattern: "sensitive", Action: "block"}}})
	_, err := (Handler{DLP: engine}).Handle(context.Background(), toolRequest("agent-a", `{"id":"sensitive"}`, true))
	var blocked *kernelerrors.Error
	if !errors.As(err, &blocked) || blocked.Code != "TOOL_DLP_BLOCKED" {
		t.Fatalf("DLP error=%v", err)
	}
}
