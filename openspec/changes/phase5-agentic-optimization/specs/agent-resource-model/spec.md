# agent-resource-model Specification (Delta)

## ADDED Requirements

### Requirement: Capability index feeds production routing
The compiled capability index SHALL feed production-equivalent agent endpoint routing, so capability filtering and scoring use the same snapshot-only index as the data plane.

#### Scenario: Index drives routing
- **WHEN** an agent task needs a capability
- **THEN** routing uses the compiled capability index
- **AND** no per-request repository access occurs
