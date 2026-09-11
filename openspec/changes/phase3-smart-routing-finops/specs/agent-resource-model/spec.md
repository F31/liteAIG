# agent-resource-model Specification

## ADDED Requirements

### Requirement: Agent version and endpoint data model
The system SHALL model Agent Versions, Endpoints, and Capabilities as tenant-scoped resources compiled into the Tenant Runtime Snapshot. A Task SHALL be able to pin a resolved Agent version; an endpoint SHALL declare its protocol, data classification, and capabilities.

#### Scenario: Version pinned in task
- **WHEN** a task references an agent with a pinned version
- **THEN** resolution uses that version's endpoint and capabilities
- **AND** the pinned version is recorded in the snapshot

#### Scenario: Capability-compiled index
- **WHEN** an agent endpoint is added
- **THEN** its capabilities appear in the snapshot capability index
- **AND** resolution can filter by capability without per-request repository access

### Requirement: Agent cost attribution readiness
Agent Version / Endpoint / Capability records SHALL carry the identifiers needed by FinOps aggregation (agent id, version, endpoint, project) so that Phase 4 agent-hop cost and federated procurement budgets can attach without re-modeling.

#### Scenario: Versioned agent usage
- **WHEN** usage is attributed to an agent
- **THEN** the version and endpoint used are preserved in the fact
- **AND** aggregation can group by version
