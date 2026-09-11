# Phase 4 — Federated Agent Trust: DoD Evidence

Status: Complete (28/28 tasks)

This change delivers the V8.2 federated agent foundation: Kernel trust boundary,
the Federated Trust model (discovery≠trust), inbound federation identity,
effective federated permission, task counters (regional/global_soft/global_hard),
sticky `crosses_trust_boundary`, external procurement cost with a dual budget,
the A2A adapter, Material Change Review, and a localized Console surface. It
satisfies Scenario F basic (cross-organization federated agent collaboration
governance).

## Capabilities Delivered

### Kernel Interaction Boundary (`kernel-interaction-boundary`)
- `interaction.Context` gains `TrustBoundary`, `Direction`, and `FederationContext`
  (relationship id, external agent id, assurance level, data boundary, trust
  anchor id); `Kind` remains four-valued — an external agent stays `Kind=agent`.
- `identity.Principal` gains `TrustBoundary`, `FederationRelationshipID`,
  `ExternalSubject`, and federated `AuthMethod` values; no second identity model.
- Snapshot federation indexes (`FederatedAgent`, `FederationRelationship`) with
  compiler + tests.

### Federated Trust Model (`federated-trust-model`)
- `internal/federation`: `Relationship`, `TrustAnchor` (JWS/JWK, mTLS/SPKI, OIDC,
  registry), `ProjectGrant`, `CapabilityGrant`, `DataBoundary`, and the lifecycle
  `discovered → candidate → pending_review → active → suspended → revoked`.
- Activation requires a verified anchor plus Project and Capability grants;
  discovery never implies trust; data boundary (status + processing regions) is
  enforced before external invocation; suspend/revoke converge.

### Inbound Federation Identity (`inbound-federation-identity`)
- `internal/federation/identity.go`: transport-level resolution (mTLS/OIDC/
  signed/registry) to a Federated Principal; remote self-claims are never
  accepted; privilege is bounded by relationship grants.

### Effective Permission and Task Governance (`federated-task-governance`)
- `EffectiveFederatedPermission` intersection evaluator (System ∩ Tenant ∩
  Project ∩ Caller ∩ Local Delegation ∩ Relationship ∩ ProjectGrant ∩
  CapabilityGrant ∩ DataBoundary).
- `TaskState` counters (`max_agent_hops/calls/cost`) with
  `regional/global_soft/global_hard`; sticky `crosses_trust_boundary`;
  `LoopDetector` terminates anomalous chains.

### External Procurement FinOps (`external-procurement-finops`)
- `internal/finops/pricing/procurement.go`: `external_agent_cost` as a distinct
  dimension; `ExternalProcurementBudget` with a both-must-pass invariant.

### A2A Adapter (`a2a-adapter`)
- `internal/access/protocol/a2a` (Agent Card discovery, Task/Message) and
  `internal/connectors/agent/a2a` (`InteractionInvoker`); Material Change Review
  in the federation lifecycle (capability/endpoint/etc. → `pending_review`, never
  auto-activate).

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

## Golden Scenario Evidence (Scenario F basic)

`tests/golden/scenario_f_test.go` proves: discovery yields a candidate (never
active); an unverified anchor cannot activate; verified anchor + grants +
data boundary activate; processing-region conflict is rejected; external
procurement cannot bypass the task total budget; `crosses_trust_boundary` is
sticky; an agent loop is terminated.

## Scope Note

The localized Console surface for Federated Agents / Trust Relationships /
External Agents is delivered as a Governance tab (`/api/admin/federation`,
read-only, zh-CN/en-US parity). Full review/suspend/revoke operations and the
procurement-cost charting are follow-up UI, consistent with the backend-first
pattern of earlier phases. All backend federation modules, the Scenario F
golden assertions, the Console surface, and the full gate set are delivered.
