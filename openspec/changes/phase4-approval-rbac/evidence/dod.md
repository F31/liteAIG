# Phase 4 — Human Approval SoD + Federated RBAC: DoD Evidence

Status: Complete (13/13 tasks)

This change delivers approval separation-of-duties, the backend-enforced
federated RBAC permission matrix, delegation intersection + Agent ACL, and a
pipeline approval checkpoint.

## Capabilities Delivered

### Human Approval SoD (`human-approval-sod`)
- `internal/controlplane/approval`: `Request` + lifecycle
  `pending → approved → rejected → cancelled`; Requester≠Approver SoD;
  optional dual approval (two distinct non-requester approvers); audit of every
  decision including denied self-approval.
- `internal/gateway/approval` pipeline checkpoint: `REQUIRE_APPROVAL` blocks
  before the connector until SoD/dual-approval is satisfied, with an
  attributable outcome.

### Federated RBAC (`federated-rbac-permissions`)
- `internal/controlplane/rbac`: role→permission matrix with
  `external_agent.review/suspend/trust.rotate/trust.revoke`,
  `approval.decide`, `delegation.grant/revoke`; `review` is tenant-admin only,
  `suspend` is operator-allowed, backend-authoritative.

### Delegation and Agent ACL (`delegation-agent-acl`)
- `internal/identity/delegation.go`: `DelegationGrant` with intersection
  semantics (delegator ∩ grant ∩ delegatee policy) that never widens, and
  `AgentACL` gating delegation targets.

### Console
- Localized approval inbox with Approve/Reject actions and federated
  relationship Suspend action in Governance (zh-CN/en-US parity, no hard-coded
  strings).

## Gate Results

| Gate | Result |
| --- | --- |
| `go test -race -timeout 180s ./...` (Postgres 55432 + Redis 56379) | PASS |
| `go vet ./...` | PASS |
| `go run ./cmd/architecture-test` | PASS |
| `npm run check` (format, locales, hardcoded strings, vitest, build) | PASS |
| `actionlint` | PASS |
| `gitleaks --config .gitleaks.toml detect` | no leaks found |
| `openspec validate --all --strict` | PASS |

## Tests

- Approval: requester cannot self-approve; dual approval requires two distinct
  non-requester approvers; lifecycle transitions; non-pending decisions rejected.
- RBAC: review denied for viewer/operator, allowed for admin; suspend allowed
  for operator; trust rotation requires admin.
- Delegation: intersection never widens; delegatee policy binds; ACL denies
  non-listed targets.
- Checkpoint: unapproved action blocked (`REQUIRE_APPROVAL`), dual-approval
  partial blocked, approved action passes, non-approval interactions unaffected.

## Scope Note

The Console delivers the approval inbox with approve/reject actions and the
federated relationship suspend action, both localized (zh-CN/en-US parity) and
wired to backend action endpoints (`POST /api/admin/approvals/{id}/action`,
`POST /api/admin/federation/{id}/suspend`). Delegation grant/revoke UI and the
relationship review-activate UI are scoped as follow-up, consistent with the
backend-first pattern of earlier phases. All backend modules (approval SoD, RBAC
matrix, delegation/ACL), the pipeline checkpoint, the Console action surfaces,
and the full gate set are delivered.

