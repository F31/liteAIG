# agentic-identity-foundation Specification (Delta)

## ADDED Requirements

### Requirement: Task linkage feeds graph reconstruction
The session/task linkage persisted on request and usage facts SHALL feed the Agent/Task graph reconstruction and per-hop cost attribution, without adding a new trace fact source.

#### Scenario: Linkage reused by the graph
- **WHEN** a request carries task and session linkage
- **THEN** the Agent/Task graph uses that linkage for reconstruction
- **AND** no second identity or trace model is introduced
