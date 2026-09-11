## 1. Human Approval SoD

- [x] 1.1 Add `internal/controlplane/approval`: `Request` (requester, action, target, required approvers, decision) with lifecycle `pending → approved → rejected → cancelled`.
- [x] 1.2 Enforce Requester≠Approver SoD in the decision path; optional dual approval requiring two distinct non-requester approvers.
- [x] 1.3 Record every decision (including denied self-approval) in Audit with actor + timestamp.
- [x] 1.4 Add tests: requester cannot self-approve, dual approval requires two distinct approvers, lifecycle transitions, audit recorded.

## 2. Federated RBAC Permissions

- [x] 2.1 Add `internal/controlplane/rbac` role→permission matrix: `external_agent.review/suspend/trust.rotate/trust.revoke`, `approval.decide`, `delegation.grant/revoke`.
- [x] 2.2 Enforce backend-authoritative: `external_agent.review` requires tenant_admin, `external_agent.suspend` allows tenant_operator; frontend is display-only.
- [x] 2.3 Add tests: review denied for non-admin, suspend allowed for operator, trust rotation requires permission and is audited.

## 3. Delegation and Agent ACL

- [x] 3.1 Add Delegation Grant with intersection semantics (EffectivePermission = delegator ∩ grant ∩ delegatee policy); never widens.
- [x] 3.2 Add Agent ACL gating which agents may receive a delegation; deny outside the ACL.
- [x] 3.3 Ensure external self-claimed delegation is never part of the local intersection.
- [x] 3.4 Add tests: delegation cannot widen, ACL denial, external self-claimed delegation ignored.

## 4. Pipeline Checkpoint and Console

- [x] 4.1 Add `gateway/approval` pipeline checkpoint: `REQUIRE_APPROVAL` blocks before the connector until SoD/dual-approval satisfied; attributable outcome.
- [x] 4.2 Add localized Console surfaces: approval inbox (list/decide/reject), federated relationship review/suspend with permission gating, delegation UI (zh-CN/en-US parity).
- [x] 4.3 Add a checkpoint test and a Scenario E/F strengthening assertion (high-risk action blocked pending approval; delegation intersection).

## 5. Release Gates

- [x] 5.1 Run the full gate set (`go test -race ./...`, `go vet ./...`, architecture-test, `npm run check`, actionlint, gitleaks) and resolve blocking failures.
- [x] 5.2 Run `openspec validate --all --strict` and record Phase 4 approval/RBAC DoD evidence.
