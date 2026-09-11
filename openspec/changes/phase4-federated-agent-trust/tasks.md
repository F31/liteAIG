## 1. Kernel Interaction Boundary

- [x] 1.1 Extend `interaction.Context` with `TrustBoundary`, `Direction`, and `FederationContext` (relationship id, external agent id, assurance level, data boundary, trust anchor id); keep `Kind` at four values (no fifth kind).
- [x] 1.2 Extend `identity.Principal` with `TrustBoundary`, `FederationRelationshipID`, `ExternalSubject`, and federated `AuthMethod` values; verify no second identity model via architecture CI.
- [x] 1.3 Add snapshot federation indexes (federated agents, relationships, anchors, grants) to `TenantRuntimeSnapshot` with compiler + validator wiring.
- [x] 1.4 Add tests: external agent stays `Kind=agent`, direction recorded, federated principal resolution, immutable snapshot indexes.

## 2. Federated Trust Model

- [x] 2.1 Add `internal/federation` domain: `Relationship`, `TrustAnchor`, `ProjectGrant`, `CapabilityGrant`, `DataBoundary` types with tenant scope.
- [x] 2.2 Implement the lifecycle state machine `discovered → candidate → pending_review → active → suspended → revoked`; discovery only produces a candidate.
- [x] 2.3 Require at least one verified Trust Anchor (JWS/JWK, mTLS/SPKI, OIDC, registry attestation) plus review plus Project/Capability grants before activation.
- [x] 2.4 Enforce Data Boundary: `data_boundary_status` vs `external_data_assurance_min` and processing-region conflicts reject before external invocation.
- [x] 2.5 Implement suspend/revoke converging within the Security Epoch.
- [x] 2.6 Add tests: discovery≠trust, unverified anchor cannot activate, grant-scoped calls, data boundary rejection, revocation.

## 3. Inbound Federation Identity

- [x] 3.1 Add inbound identity resolution in Access mapping transport credentials (mTLS/OIDC/signed identity/registry) to a Federated Principal.
- [x] 3.2 Treat remote `user_id/tenant_id/delegation` payload fields as untrusted labels; never grant internal privileges.
- [x] 3.3 Add tests: self-claimed identity ignored, transport maps to relationship, privilege bounded by grants (no internal delegation inherited).

## 4. Effective Permission and Task Governance

- [x] 4.1 Add an `EffectiveFederatedPermission` evaluator: System ∩ Tenant ∩ Project ∩ Caller ∩ Local Delegation ∩ Relationship ∩ ProjectGrant ∩ CapabilityGrant ∩ DataBoundary.
- [x] 4.2 Add Task Counter authority in coordination: `max_agent_hops`, `max_agent_calls`, `max_total_cost` with `regional/global_soft/global_hard` modes.
- [x] 4.3 Add sticky `crosses_trust_boundary` flag (set once, never reset) and loop detection terminating anomalous chains.
- [x] 4.4 Add tests: effective-permission intersection, hop/call/cost limits block next call, regional vs global_soft vs global_hard behavior, loop terminated, sticky boundary flag.

## 5. External Procurement FinOps

- [x] 5.1 Record `external_agent_cost` as a distinct procurement dimension per relationship/vendor while including it in the task total.
- [x] 5.2 Add an External Procurement Budget with a both-must-pass invariant (procurement exhaustion cannot relax the Task Total Budget).
- [x] 5.3 Add tests: procurement cost separate and included, procurement budget cannot bypass task total.

## 6. A2A Adapter

- [x] 6.1 Add `internal/access/protocol/a2a` (Agent Card discovery, Task/Message/Artifact, optional JWS signature) normalizing into the pipeline.
- [x] 6.2 Add `internal/connectors/agent` implementing the existing `InteractionInvoker`.
- [x] 6.3 Add Material Change Review: endpoint/auth/capability/publisher/trust-key/data-boundary/pricing changes transition to `pending_review` and never auto-activate.
- [x] 6.4 Add tests: discovery yields candidate only, invocation flows through governance, capability change requires review.

## 7. Console and Release Gates

- [x] 7.1 Add localized Console surfaces for Federated Agents, Trust Relationships, Trust Anchors, Grants, Data Boundary, and external procurement cost (zh-CN/en-US parity, no hard-coded strings).
- [x] 7.2 Add Scenario F basic golden assertions: outbound/inbound federation, unverified anchor blocked, grant/data-boundary enforced, external cost dual-budget, sticky boundary, loop detection.
- [x] 7.3 Run the full gate set (`go test -race ./...`, `go vet ./...`, architecture-test, `npm run check`, actionlint, gitleaks) and resolve blocking failures.
- [x] 7.4 Run `openspec validate --all --strict` and record Phase 4 (federation slice) DoD evidence.
