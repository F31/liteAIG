# Phase 4 — Human Approval SoD + Federated RBAC

## Why

V8.2's governance model requires high-risk actions to be separated by duty: the requester of an approval MUST NOT be its own approver, and Enterprise deployments MAY require dual approval. It also requires dedicated federated RBAC permissions so that trust decisions are bounded to the right roles (`external_agent.review` is Tenant Admin-only while `external_agent.suspend` may be exercised by an Operator), and it requires Delegation Grants / Agent ACL so internal handoffs can only shrink privilege. Today the codebase has the `KindApproval` enum and the federation trust model, but no Approval lifecycle with SoD, no federated permission set, and no Delegation/Agent ACL enforcement.

## What Changes

- Add a Human Approval Handoff: approval requests tied to a task/action, a lifecycle (pending → approved → rejected → cancelled), audit of every decision, and **Separation of Duties** — the requester MUST NOT approve their own request; Enterprise MAY require a second approver (dual approval).
- Add federated RBAC permissions: `external_agent.review`, `external_agent.suspend`, `external_agent.trust.rotate`, `external_agent.trust.revoke`, `approval.decide`, `delegation.grant`, `delegation.revoke` — with `external_agent.review` restricted to Tenant Admin, `external_agent.suspend` available to Tenant Operator, and the rest per the role matrix.
- Add Delegation Grant / Agent ACL: an agent handoff applies intersection semantics (EffectivePermission = delegator's permission ∩ grant ∩ delegatee's policy), so privilege can only shrink; Agent ACL gates which agent may be delegated to.
- Wire Approval into the pipeline as an `approval` interaction checkpoint: `REQUIRE_APPROVAL` blocks execution until the decision satisfies SoD/dual-approval.

## Capabilities

### New Capabilities
- `human-approval-sod`: approval lifecycle with Requester≠Approver SoD, optional dual approval, and audited decisions.
- `federated-rbac-permissions`: federated agent + approval + delegation permission set enforced backend-side.
- `delegation-agent-acl`: delegation grants with intersection semantics and agent ACL gating.

### Modified Capabilities
- `kernel-interaction-boundary`: the `approval` interaction now carries an approval request/decision in the pipeline context.

## Impact

- **Backend**: `internal/governance` (or `internal/controlplane/approval`) for the approval lifecycle + SoD; a role/permission matrix with an enforcement point; delegation grant + agent ACL in the identity/federation domains; pipeline checkpoint for `REQUIRE_APPROVAL`.
- **APIs/Console**: approval inbox (list/decide/reject), federated relationship review/suspend actions with the new permissions, delegation grant UI (localized).
- **Dependencies**: no new third-party runtime dependencies.
- **Tests**: requester cannot self-approve, dual approval requires two distinct approvers, permission enforcement (review=admin only, suspend=operator allowed), delegation intersection only shrinks, agent ACL denies unauthorized delegation.

### Non-Goals
- Semantic Cache, OIDC/SAML/SCIM, ClickHouse sink, Multi-AZ/DR, Groundedness/LLM-as-Judge, gRPC Extension Bridge (separate Phase 4 remainder items).

**Golden Scenario:** strengthens Scenario F (delegation intersection, high-risk handoff approval) and Scenario E (high-risk tool/agent actions require approval).
