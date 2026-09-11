# Phase 4 — Federated Agent Trust (Cross-Organization A2A Governance)

## Why

V8.2's defining upgrade over V8.0 is making External/Federated Agents a first-class governance object: an external partner Agent is a Tenant-level trusted projection, not a disguised internal Agent, and "discovery ≠ trust". Today the codebase has no federation layer at all — no Trust Boundary on the interaction context, no Trust Relationship/Anchor, no Project/Capability Grant, no inbound identity resolution, no external procurement cost, and no `crosses_trust_boundary` task semantics. This change delivers the Federated Agent Trust foundation and the cross-organization Scenario F basics that everything downstream (federation RBAC, approvals, DR) builds on.

## What Changes

- Extend the Kernel interaction model with `TrustBoundary` (internal/external_federated), `InteractionDirection` (inbound/outbound), `FederationContext` (relationship, external agent, assurance level, data boundary, trust anchor), and `DelegationContext` placeholders — without adding a fifth InteractionKind.
- Add a Federated Agent Trust model: `FEDERATED_AGENT` resource (`INTERNAL`/`EXTERNAL_FEDERATED`), Trust Relationship, Verified Trust Anchor (JWS/JWK, mTLS/SPKI, OIDC, registry attestation), Project Grant, Capability Grant, Data Boundary, and a lifecycle `discover → candidate → review → activate → use → suspend/revoke` where discovery never implies trust.
- Resolve Inbound External identities to a `Federated Principal` via transport-level trust (mTLS/OIDC/signed identity/registry); remote self-claimed `user_id/tenant_id/delegation` MUST NOT grant internal privileges.
- Enforce `EffectiveFederatedPermission = System ∩ Tenant ∩ Project ∩ Caller ∩ Local Delegation ∩ Federation Relationship ∩ Project Grant ∩ Capability Grant ∩ Data Boundary` — LiteAIG only relies on local provable authorization facts.
- Add Task Governance for federation: sticky `crosses_trust_boundary` flag, `max_agent_hops/max_agent_calls/max_total_cost` counters with `regional/global_soft/global_hard` consistency modes, and loop detection.
- Add External Agent FinOps: `external_agent_cost` (procurement) alongside the task total, a separate External Procurement Budget that cannot bypass the Task Total Budget, and External Response re-guardrailed as untrusted `external_agent_response` provenance.
- Add an A2A 1.0 Adapter (Agent Card discovery + optional JWS signature) behind the existing `Invoker` contract; Agent Card material change (endpoint, auth scheme, capability, publisher, trust key, data boundary, pricing) requires review before re-activation.

## Capabilities

### New Capabilities
- `kernel-interaction-boundary`: `TrustBoundary`/`Direction`/`FederationContext` on the Kernel interaction model (no fifth InteractionKind).
- `federated-trust-model`: Federated Agent Relationship, Trust Anchor, Project/Capability Grant, Data Boundary, and the discover≠trust lifecycle.
- `inbound-federation-identity`: transport-level inbound identity resolution to a Federated Principal (remote self-claims never trusted).
- `federated-task-governance`: sticky `crosses_trust_boundary`, hop/call/cost counters with regional/global_soft/global_hard modes, loop control.
- `external-procurement-finops`: external agent procurement cost + External Procurement Budget (dual-budget, cannot bypass Task Total).
- `a2a-adapter`: A2A 1.0 wire adapter (Agent Card discovery/signature, Task/Message) behind the Invoker contract.

### Modified Capabilities
- `organization-attribution`: adds `external_agent_response` content provenance and federated relationship attribution.
- `finops-pricing-chargeback`: adds external procurement cost as a distinct dimension.

## Impact

- **Backend**: Kernel `interaction.Context` + `identity.Principal` extension; new `internal/federation` domain (relationship/anchor/grant/boundary/lifecycle); inbound identity resolution in Access; Task counter authority in coordination (regional/global_soft/global_hard); procurement budget + cost in FinOps; A2A adapter in `internal/access/protocol/a2a` + `internal/connectors/agent`.
- **APIs/Console**: Federated Agents / Trust Relationships / Trust Anchors / Grants / Data Boundary review surfaces; external agent cost in FinOps (localized).
- **Dependencies**: no new third-party runtime dependencies.
- **Tests**: discovery≠trust, unverified anchor cannot activate, inbound self-claim rejected, effective-permission intersection, procurement budget cannot bypass task total, sticky crosses_trust_boundary, regional/global counters, material-change review.

### Non-Goals
- Multi-AZ / Region DR, Semantic Cache, OIDC/SAML/SCIM, ClickHouse sink, Delegation/Approval full workflow, Capability Routing, Multi-Agent Planner (Phase 4 remainder / Phase 5).
- Running any Agent Workflow: LiteAIG only governs A2A/MCP connectivity, identity, budget, and audit.

**Golden Scenario:** Scenario F (cross-organization Federated Agent collaboration governance) — outbound/inbound federation, trust anchors, grants, residency, external cost, revocation, task budget, loop, and cross-region consistency basics.
