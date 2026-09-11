# Phase 4 — Human Approval SoD + Federated RBAC: Design

## Context

The Kernel interaction model has `KindApproval` but no approval lifecycle, no
Separation of Duties, and no dual approval. The federation domain
(`internal/federation`) owns trust relationships but has no permission set that
distinguishes Tenant Admin review from Tenant Operator suspend. There is no
Delegation Grant or Agent ACL; handoffs are unmodeled. This change delivers the
approval SoD, the federated RBAC permission set, and delegation/agent ACL as the
governance closure for Phase 4.

## Goals / Non-Goals

**Goals:**
- Approval lifecycle with Requester≠Approver SoD and optional dual approval,
  fully audited.
- A backend-enforced permission set for federated agent + approval + delegation
  actions with the V8.2 role matrix.
- Delegation grants with intersection semantics (privilege only shrinks) and
  Agent ACL gating.
- A `REQUIRE_APPROVAL` checkpoint in the pipeline that blocks execution until
  the decision satisfies SoD/dual-approval.

**Non-Goals:**
- Semantic Cache, OIDC/SAML/SCIM, ClickHouse sink, Multi-AZ/DR, Groundedness/
  LLM-as-Judge, gRPC Extension Bridge.

## Decisions

### 1. Approval is a tenant-scoped stateful object with SoD
New `internal/controlplane/approval`: `Request` (id, tenant, requester, action,
target, required approvers, decision), lifecycle `pending → approved → rejected →
cancelled`. SoD: a requester can never approve their own request; if
`DualApproval=true`, at least two distinct approvers (neither the requester) must
approve. Every decision writes an audit event with actor + timestamp.
Alternative considered: trusting a single approver. Rejected — V8.2 requires
Requester≠Approver and optional dual approval.

### 2. Permissions are a backend matrix, not a frontend flag
New `internal/controlplane/rbac` (or inside `adminapi`): a role→permission map
with the V8.2 permissions (`external_agent.review/suspend/trust.rotate/
trust.revoke`, `approval.decide`, `delegation.grant/revoke`). `external_agent.review`
requires the `tenant_admin` role; `external_agent.suspend` allows `tenant_operator`.
Enforcement is backend-side; the frontend only hides what the backend denies.
Alternative considered: frontend-only gating. Rejected — V8.2 §26 mandates
backend-authoritative authorization.

### 3. Delegation is intersection-only and ACL-gated
Delegation grants live in `internal/identity` (or the federation domain for
federated targets): `EffectiveDelegatedPermission = delegator.permission ∩
grant.permission ∩ delegatee.policy ∩ agentACL`. Agent ACL enumerates which
agents may receive a delegation; any handoff outside it is denied. There is no
privilege escalation path.
Alternative considered: additive handoffs. Rejected — V8.2 §1.9 rule 6.

### 4. The approval checkpoint is a pipeline stage hook
A `gateway/approval` handler checks `RequestContext` for a pending
`REQUIRE_APPROVAL` action; if the approval is not satisfied (approved by a
non-requester, or dual not met), execution is blocked before the connector with
an attributable outcome. This keeps the fixed seven-stage pipeline intact — the
approval decision is a governance fact, not a new stage.

## Risks / Trade-offs

- [Self-approval bypass] → SoD enforced in the decision path + test.
- [Dual approval liveness] → optional per policy; single-approver mode preserves flow when Enterprise dual approval is off.
- [Delegation widening] → intersection evaluator + agent ACL + regression tests.
- [Frontend hiding but backend permissive] → backend enforces; frontend is display-only.

## Migration Plan

1. Add `internal/controlplane/approval` (request + lifecycle + SoD + dual approval + audit) with tests.
2. Add `internal/controlplane/rbac` permission matrix + enforcement with tests.
3. Add Delegation Grant + Agent ACL in `internal/identity` (or federation for federated targets) with intersection evaluator + tests.
4. Add `gateway/approval` pipeline checkpoint (REQUIRE_APPROVAL) + test.
5. Add localized Console surfaces (approval inbox, relationship review/suspend with permissions, delegation UI).
6. Run full gate set + OpenSpec strict validation + record DoD evidence.

Rollback: approval/dual-approval behind config defaults (single-approver + no SoD
check preserved when disabled); RBAC defaults to the existing authorizer behavior;
delegation/ACL disabled when no grants exist.
