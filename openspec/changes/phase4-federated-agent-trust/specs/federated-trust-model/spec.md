# federated-trust-model Specification

## ADDED Requirements

### Requirement: Discovery is never trust
Agent discovery SHALL only produce a candidate. An External/Federated Agent SHALL NOT be callable until it has an active Trust Relationship with at least one verified Trust Anchor, human review, Data Boundary review, and the applicable Project and Capability Grants. The lifecycle SHALL be `discovered → candidate → pending_review → active → suspended → revoked`.

#### Scenario: Discovered agent is not callable
- **WHEN** a partner Agent Card is discovered but no relationship exists
- **THEN** the agent is a candidate only
- **AND** any call attempt is denied

#### Scenario: Active requires verified anchor
- **WHEN** a relationship has no verified Trust Anchor
- **THEN** it cannot become active
- **AND** an unverified anchor is never sufficient alone

### Requirement: Verified Trust Anchor abstraction
A Trust Relationship SHALL reference at least one verified Trust Anchor chosen from JWS/JWK, mTLS/SPKI pin, OIDC issuer+subject, or trusted registry attestation. The system MUST NOT treat an Agent Card JWS signature as the only acceptable trust root.

#### Scenario: Anchor type pluggability
- **WHEN** a relationship is verified via an mTLS/SPKI pin instead of JWS
- **THEN** it satisfies the verified-anchor requirement
- **AND** the anchor type is recorded

### Requirement: Project and capability grants
An external agent's allowed scope SHALL be controlled by Tenant-level Project Grants and Capability Grants, not by membership in an internal Project. A Project grant authorizes which Projects may call the external agent; a Capability grant authorizes which capabilities are exposed.

#### Scenario: Grant-scoped external call
- **WHEN** an internal agent calls an external agent
- **THEN** the call requires an active Project Grant and the requested capability is within the Capability Grant
- **AND** calls outside the grants are denied

### Requirement: Data boundary enforcement
An external relationship SHALL carry `data_boundary_status` (`unknown` | `declared` | `contractually_bound`), processing regions, retention, training-use, and subprocessor/DPA facts. A Project MAY set `external_data_assurance_min`; when the relationship's status is below the minimum or a processing region conflicts with the Project's allowed regions, the call SHALL be rejected before data leaves the boundary.

#### Scenario: Unknown processing region rejected
- **WHEN** a Project requires `contractually_bound` assurance but the relationship reports `unknown`
- **THEN** the call is rejected before any external invocation

### Requirement: Suspension and revocation
Suspending or revoking a relationship SHALL prevent new external calls and SHALL converge within the Security Epoch. Revocation SHALL terminate the relationship and its grants.

#### Scenario: Revoked relationship blocked
- **WHEN** a relationship is revoked
- **THEN** new calls are blocked
- **AND** the state converges within the Security Epoch
