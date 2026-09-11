// Package approval adapts the approval decision to the pipeline checkpoint:
// a REQUIRE_APPROVAL action blocks execution before the connector until the
// decision satisfies separation-of-duties / dual approval.
package approval

import (
	"context"

	"github.com/F31/liteAIG/internal/kernel"
	kernelerrors "github.com/F31/liteAIG/internal/kernel/errors"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/pipeline"
)

// Decision is the approval state for an action.
type Decision struct {
	Action     string
	Requester  string
	Approved   bool
	Rejected   bool
	Approvers  int
	DualNeeded bool
}

// Checker resolves whether an action's approval is satisfied. It receives the
// full request context so it can identify the caller and target.
type Checker interface {
	Check(context.Context, *kernel.RequestContext) (*Decision, error)
}

// Handler is the pipeline checkpoint for approval-gated actions.
type Handler struct {
	Checker Checker
}

// requiresApproval reports whether the compiled policy gates this interaction:
// explicit approval-kind interactions always, tool calls when the project's
// ToolPolicy requires approval, agent calls when the agent requires approval.
func (h Handler) requiresApproval(request *kernel.RequestContext) bool {
	if request.Snapshot == nil {
		return false
	}
	target := request.Interaction.Target.ID
	switch request.Interaction.Kind {
	case interaction.KindApproval:
		return true
	case interaction.KindTool:
		policy, ok := request.Snapshot.ToolPolicy(request.ProjectID(), target)
		return ok && policy.RequireApproval
	case interaction.KindAgent:
		agent, ok := request.Snapshot.Agent(target)
		return ok && agent.RequireApproval
	}
	return false
}

// Handle blocks execution when the action requires approval that is not yet
// satisfied.
func (h Handler) Handle(ctx context.Context, request *kernel.RequestContext) (pipeline.Directive, error) {
	if h.Checker == nil || request == nil || request.Interaction == nil {
		return pipeline.Continue, nil
	}
	if !h.requiresApproval(request) {
		return pipeline.Continue, nil
	}
	decision, err := h.Checker.Check(ctx, request)
	if err != nil {
		return pipeline.Continue, err
	}
	if decision.Rejected {
		return pipeline.Continue, &kernelerrors.Error{Code: "REQUIRE_APPROVAL", Message: "action was rejected", RequestID: request.RequestID}
	}
	if !decision.Approved {
		return pipeline.Continue, &kernelerrors.Error{Code: "REQUIRE_APPROVAL", Message: "action awaits approval", RequestID: request.RequestID}
	}
	if decision.DualNeeded && decision.Approvers < 2 {
		return pipeline.Continue, &kernelerrors.Error{Code: "REQUIRE_APPROVAL", Message: "action awaits dual approval", RequestID: request.RequestID}
	}
	return pipeline.Continue, nil
}

var _ pipeline.Handler = Handler{}
