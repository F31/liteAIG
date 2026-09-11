package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	approval "github.com/F31/liteAIG/internal/controlplane/approval"
	gatewayapproval "github.com/F31/liteAIG/internal/gateway/approval"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/tenancy"
)

// approvalGate adapts the control-plane approval service to the data-plane
// checkpoint. It resolves the request idempotently by (target, caller): an
// existing request in any status wins, and only a first-time action creates a
// pending request. Rejected requests stay blocked (fail closed) until a new
// request supersedes them.
type approvalGate struct {
	service *approval.Service
}

func (g *approvalGate) Check(ctx context.Context, request *kernel.RequestContext) (*gatewayapproval.Decision, error) {
	scope := tenancy.TenantScope{TenantID: request.TenantID()}
	caller := request.Interaction.Caller.ID
	target := request.Interaction.Target.ID
	decision := &gatewayapproval.Decision{Action: target, Requester: caller}

	existing, err := g.service.Find(ctx, scope, target, caller)
	if err != nil && !errors.Is(err, approval.ErrNotFound) {
		return nil, err
	}
	if err == nil {
		decision.Approved = existing.Status == approval.StatusApproved
		decision.Rejected = existing.Status == approval.StatusRejected
		decision.Approvers = len(existing.ApprovedBy)
		decision.DualNeeded = existing.DualApproval
		return decision, nil
	}

	created, err := g.service.Create(ctx, scope, approval.Request{
		ID:        stableApprovalID(scope.TenantID, caller, target),
		Requester: caller,
		Action:    target,
		Target:    target,
	})
	if err != nil {
		return nil, err
	}
	decision.Approved = false
	decision.DualNeeded = created.DualApproval
	return decision, nil
}

// stableApprovalID derives a deterministic version-5-style UUID from
// (tenant, caller, target) so concurrent first-time calls converge on one
// pending row instead of duplicating requests.
func stableApprovalID(tenantID, caller, target string) string {
	digest := sha256.Sum256([]byte(tenantID + "\x00" + caller + "\x00" + target))
	b := digest[:16]
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

var _ gatewayapproval.Checker = (*approvalGate)(nil)
