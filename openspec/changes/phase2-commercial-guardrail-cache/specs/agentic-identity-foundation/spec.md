# agentic-identity-foundation Specification

## ADDED Requirements

### Requirement: Agent identity on the canonical principal
Agent SHALL be a type of the canonical Principal model, resolved through the same Identity/Auth path as User, Application, and Service Account. There SHALL NOT be a second agent-specific identity subsystem. A snapshot `AgentIndex` SHALL record agent identity, status, and binding without embedding provider or credential material.

#### Scenario: Agent principal resolution
- **WHEN** an Agent invokes the gateway
- **THEN** the caller is resolved as `Principal.Type=agent` with its `AgentID`
- **AND** the same authorization and attribution path applies as for other principal types

#### Scenario: No duplicate identity model
- **WHEN** architecture CI analyzes agent identity handling
- **THEN** no package declares a second top-level Agent identity model
- **AND** agent metadata belongs to the Agent Registry, not to Identity

### Requirement: Task ID plumbing
`task_id`, `root_task_id`, and `parent_task_id` SHALL propagate from the `InteractionContext` through Admission, Accounting facts, Request Explorer records, Usage records, and OTel span attributes so multi-step tasks are reconstructable end to end.

#### Scenario: Task IDs preserved in accounting
- **WHEN** a request carries a task identifier
- **THEN** the Accounting record and Usage record retain the task, root task, and parent task IDs
- **AND** Request Explorer can filter and correlate by task

#### Scenario: Task correlation in telemetry
- **WHEN** a request completes
- **THEN** its OTel spans carry the task, root task, and parent task attributes
- **AND** no separate trace fact source is created for task linkage

### Requirement: Session and task linkage readiness
The foundation SHALL record session identifiers with requests so that later phases can reconstruct agent call graphs, while this phase SHALL NOT yet build the Agent Graph or multi-agent observability.

#### Scenario: Session recorded per request
- **WHEN** any request has a session identifier
- **THEN** the session identifier is retained in the request record and Tool Call Event linkage
- **AND** no cross-session content is exposed
