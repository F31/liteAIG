# agent-capability-routing Specification

## ADDED Requirements

### Requirement: Capability-based agent routing
The system SHALL resolve an agent task to an endpoint by capability using the compiled capability index, then order eligible endpoints by a bounded normalized score (health, cost, latency). The same deterministic plan SHALL be used by production and the explainer.

#### Scenario: Capability filters endpoints
- **WHEN** a task requires a capability
- **THEN** only endpoints exposing that capability are eligible
- **AND** the plan records the capability evidence

#### Scenario: Score orders eligible endpoints
- **WHEN** multiple endpoints expose the capability
- **THEN** they are ordered by the bounded health/cost/latency score
- **AND** the selected and fallback endpoints are attributable

### Requirement: Evidence-windowed scoring, no privilege
Each AgentScore SHALL carry an evidence window and SHALL be explainable. A score SHALL influence routing order only and SHALL NEVER grant elevated privilege.

#### Scenario: Score is explainable and bounded
- **WHEN** an agent is scored
- **THEN** the score components and evidence window are available
- **AND** no score auto-grants higher permission
