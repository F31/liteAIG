# kernel-interaction-boundary Specification

## ADDED Requirements

### Requirement: Trust boundary on the interaction model
The Kernel interaction context SHALL carry a `TrustBoundary` (`internal` | `external_federated`), an `InteractionDirection` (`internal` | `inbound` | `outbound`), and a `FederationContext` (relationship id, external agent id, assurance level, data boundary, trust anchor id). This SHALL be additive to the existing model: an external agent remains `Kind=agent` with `TrustBoundary=external_federated`, and there SHALL NOT be a fifth InteractionKind.

#### Scenario: External agent stays kind agent
- **WHEN** an interaction targets an external federated agent
- **THEN** `Kind` is `agent` and `TrustBoundary` is `external_federated`
- **AND** no new InteractionKind is introduced

#### Scenario: Direction recorded
- **WHEN** an external partner agent calls an internal agent
- **THEN** the interaction direction is `inbound`
- **AND** the resolved principal carries the federation relationship id

### Requirement: Canonical principal carries federation fields
The canonical Principal SHALL carry `TrustBoundary`, `FederationRelationshipID`, and `ExternalSubject`, with `AuthMethod` values for federated transports (mTLS, federated OIDC, federated JWS, registry attestation). There SHALL NOT be a second identity model for external agents.

#### Scenario: Federated principal resolution
- **WHEN** an inbound external agent is resolved
- **THEN** it becomes `Principal.Type=agent` with `TrustBoundary=external_federated` and its relationship id
- **AND** the same authorization/attribution path applies as for other principals
