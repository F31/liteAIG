package approval

import (
	"context"
	"errors"
	"testing"

	"github.com/F31/liteAIG/internal/kernel"
	kernelerrors "github.com/F31/liteAIG/internal/kernel/errors"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
)

type checker struct {
	decisions map[string]*Decision
	err       error
}

func (c checker) Check(_ context.Context, request *kernel.RequestContext) (*Decision, error) {
	if c.err != nil {
		return nil, c.err
	}
	decision, ok := c.decisions[request.Interaction.Target.ID]
	if !ok {
		return nil, errors.New("approval not found")
	}
	decision.Action = request.Interaction.Target.ID
	decision.Requester = request.Interaction.Caller.ID
	return decision, nil
}

func approvalRequest(action string) *kernel.RequestContext {
	return &kernel.RequestContext{RequestID: "r", Snapshot: runtime.NewTenantSnapshot(runtime.TenantSnapshotData{TenantID: "t"}), Interaction: &interaction.Context{Kind: interaction.KindApproval, TenantID: "t", Target: interaction.ResourceRef{Type: "action", ID: action}}}
}

func toolPolicyRequest(requireApproval bool) *kernel.RequestContext {
	snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID:     "t",
		Tools:        []runtime.Tool{{ID: "tool", Name: "payment.execute", Status: "active"}},
		ToolPolicies: []runtime.ToolPolicy{{ToolID: "tool", ProjectID: "project", Allowed: true, RequireApproval: requireApproval}},
	})
	return &kernel.RequestContext{RequestID: "r", Snapshot: snapshot, Interaction: &interaction.Context{Kind: interaction.KindTool, TenantID: "t", ProjectID: "project", Caller: interaction.PrincipalRef{Type: "agent", ID: "agent-a"}, Target: interaction.ResourceRef{Type: "tool", ID: "tool"}}}
}

func agentRequest(requireApproval bool) *kernel.RequestContext {
	snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "t",
		Agents:   []runtime.Agent{{ID: "agent-x", Name: "buyer", Status: "active", RequireApproval: requireApproval}},
	})
	return &kernel.RequestContext{RequestID: "r", Snapshot: snapshot, Interaction: &interaction.Context{Kind: interaction.KindAgent, TenantID: "t", Caller: interaction.PrincipalRef{Type: "human", ID: "user-1"}, Target: interaction.ResourceRef{Type: "agent", ID: "agent-x"}}}
}

func TestUnapprovedActionBlocked(t *testing.T) {
	handler := Handler{Checker: checker{decisions: map[string]*Decision{"payment.execute": {Approved: false}}}}
	_, err := handler.Handle(context.Background(), approvalRequest("payment.execute"))
	var blocked *kernelerrors.Error
	if !errors.As(err, &blocked) || blocked.Code != "REQUIRE_APPROVAL" {
		t.Fatalf("unapproved action error = %v", err)
	}
}

func TestDualApprovalNotYetSatisfiedBlocked(t *testing.T) {
	handler := Handler{Checker: checker{decisions: map[string]*Decision{"trust.rotate": {Approved: true, Approvers: 1, DualNeeded: true}}}}
	_, err := handler.Handle(context.Background(), approvalRequest("trust.rotate"))
	var blocked *kernelerrors.Error
	if !errors.As(err, &blocked) || blocked.Code != "REQUIRE_APPROVAL" {
		t.Fatalf("dual-approval partial error = %v", err)
	}
}

func TestApprovedActionPasses(t *testing.T) {
	handler := Handler{Checker: checker{decisions: map[string]*Decision{"trust.rotate": {Approved: true, Approvers: 2, DualNeeded: true}}}}
	if _, err := handler.Handle(context.Background(), approvalRequest("trust.rotate")); err != nil {
		t.Fatalf("approved action blocked: %v", err)
	}
}

func TestNonApprovalInteractionPasses(t *testing.T) {
	handler := Handler{Checker: nil} // no checker configured
	request := &kernel.RequestContext{RequestID: "r", Interaction: &interaction.Context{Kind: interaction.KindTool, TenantID: "t"}}
	if _, err := handler.Handle(context.Background(), request); err != nil {
		t.Fatalf("non-approval interaction blocked: %v", err)
	}
}

func TestCheckerErrorPropagates(t *testing.T) {
	sentinel := errors.New("store down")
	handler := Handler{Checker: checker{err: sentinel}}
	if _, err := handler.Handle(context.Background(), approvalRequest("payment.execute")); !errors.Is(err, sentinel) {
		t.Fatalf("checker error = %v", err)
	}
}

func TestRejectedActionBlocked(t *testing.T) {
	handler := Handler{Checker: checker{decisions: map[string]*Decision{"payment.execute": {Rejected: true}}}}
	_, err := handler.Handle(context.Background(), approvalRequest("payment.execute"))
	var blocked *kernelerrors.Error
	if !errors.As(err, &blocked) || blocked.Code != "REQUIRE_APPROVAL" {
		t.Fatalf("rejected action error = %v", err)
	}
}

func TestToolWithoutApprovalPolicyPasses(t *testing.T) {
	handler := Handler{Checker: checker{err: errors.New("checker must not be consulted")}}
	if _, err := handler.Handle(context.Background(), toolPolicyRequest(false)); err != nil {
		t.Fatalf("ungated tool blocked: %v", err)
	}
}

func TestToolWithApprovalPolicyEnforcesDecision(t *testing.T) {
	blocked := Handler{Checker: checker{decisions: map[string]*Decision{"tool": {Approved: false}}}}
	_, err := blocked.Handle(context.Background(), toolPolicyRequest(true))
	var approval *kernelerrors.Error
	if !errors.As(err, &approval) || approval.Code != "REQUIRE_APPROVAL" {
		t.Fatalf("gated tool error = %v", err)
	}
	approved := Handler{Checker: checker{decisions: map[string]*Decision{"tool": {Approved: true}}}}
	if _, err := approved.Handle(context.Background(), toolPolicyRequest(true)); err != nil {
		t.Fatalf("approved tool blocked: %v", err)
	}
}

func TestAgentWithApprovalPolicyEnforcesDecision(t *testing.T) {
	blocked := Handler{Checker: checker{decisions: map[string]*Decision{"agent-x": {Approved: false}}}}
	_, err := blocked.Handle(context.Background(), agentRequest(true))
	var approval *kernelerrors.Error
	if !errors.As(err, &approval) || approval.Code != "REQUIRE_APPROVAL" {
		t.Fatalf("gated agent error = %v", err)
	}
	approved := Handler{Checker: checker{decisions: map[string]*Decision{"agent-x": {Approved: true}}}}
	if _, err := approved.Handle(context.Background(), agentRequest(true)); err != nil {
		t.Fatalf("approved agent blocked: %v", err)
	}
}
