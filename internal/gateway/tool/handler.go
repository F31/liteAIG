// Package tool enforces Tool ACL, schema, and DLP before MCP invocation.
package tool

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/F31/liteAIG/internal/guardrail/builtin"
	"github.com/F31/liteAIG/internal/kernel"
	kernelerrors "github.com/F31/liteAIG/internal/kernel/errors"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/pipeline"
)

type Handler struct{ DLP *builtin.Engine }

func (h Handler) Handle(_ context.Context, request *kernel.RequestContext) (pipeline.Directive, error) {
	if request == nil || request.Interaction == nil || request.Interaction.Kind != interaction.KindTool {
		return pipeline.Continue, nil
	}
	if request.Snapshot == nil || request.Request == nil || request.Request.Tool == nil {
		return pipeline.Continue, errors.New("tool request is incomplete")
	}
	toolID := request.Interaction.Target.ID
	tool, ok := request.Snapshot.Tool(toolID)
	if !ok {
		return pipeline.Continue, &kernelerrors.Error{Code: "TOOL_NOT_FOUND", Message: "tool not available", RequestID: request.RequestID}
	}
	policy, ok := request.Snapshot.ToolPolicy(request.ProjectID(), toolID)
	if !ok || !policy.Allowed || !agentAllowed(policy.AllowedAgentIDs, request.Interaction.Caller) {
		return pipeline.Continue, &kernelerrors.Error{Code: "TOOL_FORBIDDEN", Message: "tool is not allowed", RequestID: request.RequestID}
	}
	if err := validateRequired(tool.Schema, request.Request.Tool.Arguments); err != nil {
		return pipeline.Continue, &kernelerrors.Error{Code: "TOOL_SCHEMA_INVALID", Message: "tool arguments do not match schema", RequestID: request.RequestID}
	}
	if h.DLP != nil && h.DLP.Evaluate(string(request.Request.Tool.Arguments)).Blocked {
		return pipeline.Continue, &kernelerrors.Error{Code: "TOOL_DLP_BLOCKED", Message: "tool arguments blocked by policy", RequestID: request.RequestID}
	}
	return pipeline.Continue, nil
}

func agentAllowed(allowed []string, caller interaction.PrincipalRef) bool {
	if len(allowed) == 0 {
		return true
	}
	if caller.Type != "agent" {
		return false
	}
	for _, id := range allowed {
		if id == caller.ID {
			return true
		}
	}
	return false
}

func validateRequired(schema, arguments []byte) error {
	if len(schema) == 0 {
		return nil
	}
	var definition struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schema, &definition); err != nil {
		return err
	}
	var values map[string]any
	if err := json.Unmarshal(arguments, &values); err != nil {
		return err
	}
	for _, field := range definition.Required {
		if _, ok := values[field]; !ok {
			return errors.New("required field missing")
		}
	}
	return nil
}

var _ pipeline.Handler = Handler{}
