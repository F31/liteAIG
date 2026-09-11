# inbound-federation-identity Specification

## ADDED Requirements

### Requirement: Transport-level inbound identity
An inbound A2A call SHALL be resolved to a Federated Principal using transport-layer trust: mTLS certificate, federated OIDC, a signed identity, or a trusted registry attestation. Remote payload fields (`user_id`, `tenant_id`, `delegation`) SHALL be treated as untrusted labels and MUST NOT grant internal User/Delegation privileges.

#### Scenario: Self-claimed identity ignored
- **WHEN** an inbound external agent payload claims an internal user id and delegation
- **THEN** those claims do not affect authorization
- **AND** the principal is derived only from transport-level identity

#### Scenario: Transport maps to relationship
- **WHEN** an inbound call presents a valid federated mTLS certificate
- **THEN** it resolves to the associated Federation Relationship
- **AND** authorization uses the relationship grants, not the payload

### Requirement: No cross-boundary privilege
An external federated principal SHALL receive no more than its Relationship, Project Grant, Capability Grant, and Data Boundary allow. It MUST NOT inherit internal Agent memberships or internal Delegation grants.

#### Scenario: Privilege is bounded by grants
- **WHEN** an external principal invokes an internal agent
- **THEN** effective permission is the local intersection of the relationship grants
- **AND** no internal delegation is inherited
