# Phase 4 — Federated Agent Trust: Design

## Context

The Kernel interaction model (`internal/kernel/interaction/types.go`) currently
carries `Kind/Protocol/Tenant/Project/Session/Task/Caller/Target` but has no
Trust Boundary, Direction, or Federation context. `identity.Principal` has
`AgentID/AttributionTrust` but no federated fields. The coordination layer has
an atomic Budget Ledger and ZSET leases, but no task counters with
regional/global modes. FinOps has pricing/aggregation but no external
procurement dimension. This change adds the Federated Agent Trust layer as a
governance-first foundation for cross-organization A2A.

## Goals / Non-Goals

**Goals:**
- Kernel interaction model gains `TrustBoundary`, `Direction`, `FederationContext`
  without a fifth InteractionKind.
- A tenant-scoped Federated Trust model where discovery never implies trust:
  Relationship → Verified Trust Anchor → Project/Capability Grant → Data Boundary
  → activate → use → suspend/revoke.
- Inbound external identities resolve to a Federated Principal via transport
  trust; remote self-claims never grant internal privileges.
- Effective federated permission is a local intersection; LiteAIG never assumes
  knowledge of the partner's internal policy.
- Task governance: sticky `crosses_trust_boundary`, hop/call/cost counters in
  regional/global_soft/global_hard modes, loop detection.
- External procurement cost is a distinct FinOps dimension with its own budget
  that cannot bypass the Task Total Budget.

**Non-Goals:**
- Multi-AZ/DR, Semantic Cache, OIDC/SAML/SCIM, ClickHouse sink, full
  Delegation/Approval workflow, Capability Routing, Agent Planner/Workflow.

## Decisions

### 1. Kernel extension is additive and keeps four InteractionKinds
Add to `interaction.Context`: `TrustBoundary`, `Direction`, and a
`FederationContext` (RelationshipID, ExternalAgentID, AssuranceLevel,
DataBoundary, TrustAnchorID). Keep `Kind` as one of model/tool/agent/approval —
an external agent is `Kind=agent` + `TrustBoundary=external_federated`, not a
new kind. Extend `identity.Principal` with `TrustBoundary`,
`FederationRelationshipID`, `ExternalSubject`, and `AuthMethod` values for
federated transports.
Alternative considered: a fifth InteractionKind. Rejected by V8.2 §1.9 rule 8.

### 2. Federated Trust is a Tenant-scoped domain with an explicit lifecycle
New `internal/federation` owns Relationship/TrustAnchor/ProjectGrant/
CapabilityGrant/DataBoundary and a lifecycle state machine:
`discovered → candidate → pending_review → active → suspended → revoked`.
Discovery only creates a candidate. Activation requires at least one verified
Trust Anchor plus human review plus Data Boundary review plus Project/Capability
grants. A Trust Anchor is any of JWS/JWK, mTLS/SPKI pin, OIDC issuer+subject, or
registry attestation — never JWS-only.
Alternative considered: trusting a discovered Agent Card directly. Rejected by
the Discovery ≠ Trust invariant.

### 3. Inbound identity resolves at the transport layer
An inbound A2A call maps transport credentials (mTLS cert / OIDC / signed
identity / registry attestation) to a Federated Principal keyed by
`FederationRelationshipID`. Any `user_id/tenant_id/delegation` in the payload is
treated as untrusted labels, never as authorization. Resolution feeds the same
Principal model the pipeline already consumes.

### 4. Effective federated permission is an intersection
A permission evaluator computes
`System ∩ Tenant ∩ Project ∩ Caller ∩ Local Delegation ∩ Relationship ∩
ProjectGrant ∩ CapabilityGrant ∩ DataBoundary`, where DataBoundary enforces
`external_data_assurance_min` (unknown/declared/contractually_bound) against the
relationship's `data_boundary_status` and processing regions. If any factor is
absent or fails, the call is denied before the downstream connector.

### 5. Task counters reuse the coordination authority
Extend `internal/platform/coordination` with a `TaskCounter` authority:
`regional` (home-region authoritative), `global_soft` (counter slices per region
with bounded overshoot + reconcile), `global_hard` (single strong-consistent
authority). Counters cover `max_agent_hops`, `max_agent_calls`, `max_total_cost`.
A `crosses_trust_boundary` flag is set once a task uses an external relationship
and is sticky (never reset) because it records an audit fact.

### 6. External procurement is a dual-budget FinOps dimension
`external_agent_cost` is recorded alongside task totals but aggregated under an
External Procurement Budget per relationship/vendor. Enforcing procurement
budget exhaustion MUST NOT relax the Task Total Budget: both must pass. This
prevents procurement budget from being a bypass of task-level cost limits.

### 7. A2A is an adapter, not a new pipeline
`internal/access/protocol/a2a` normalizes A2A 1.0 Agent Card/Task/Message into
`interaction` + the pipeline; `internal/connectors/agent` invokes remote agents
through the existing `InteractionInvoker`. Material changes to an Agent Card
(endpoint, auth scheme, capability, publisher, trust key, data boundary,
pricing) transition the relationship to `pending_review`, never auto-activate.

## Risks / Trade-offs

- [JWS-only trust] → Trust Anchor abstraction supports mTLS/OIDC/registry; JWS is one option.
- [Inbound spoofing] → transport-level identity only; self-claims rejected; regression tests.
- [Global counters latency] → `global_hard` is opt-in with RTT cost; `regional` is default.
- [Procurement bypass] → dual-budget both-must-pass invariant + test.
- [Material change auto-trust] → review gate + test that a changed endpoint/capability does not auto-activate.

## Migration Plan

1. Kernel interaction/principal extension + snapshot federation indexes (compile-only).
2. Federation domain (relationship/anchor/grant/boundary/lifecycle) with an in-memory registry + contract tests.
3. Inbound identity resolution in Access.
4. Effective-permission evaluator + Data Boundary checks.
5. Task counter authority (regional/global_soft/global_hard) in coordination; sticky crosses_trust_boundary.
6. External procurement budget + cost dimension in FinOps.
7. A2A adapter + Agent Card discovery/signature + material-change review gate.
8. Localized Console surfaces; full gate set; OpenSpec strict validation; Scenario F basic evidence.

Rollback: each capability is behind config defaults; without a verified relationship
external agents are simply unresolved (fail closed), preserving current behavior.
