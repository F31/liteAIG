# agent-optimization-recommendations Specification

## ADDED Requirements

### Requirement: Suggest-only task-level recommendations
The system SHALL generate task-level agent routing and guardrail recommendations from agent metrics with an evidence window. Accepting a recommendation SHALL create a Config Draft; production SHALL never be mutated directly.

#### Scenario: Accept creates a draft
- **WHEN** an agent routing recommendation is accepted
- **THEN** a Config Draft is created with the proposed change
- **AND** no active configuration is mutated until the draft is validated and published

### Requirement: Read-only graph analytics
The system SHALL derive per-agent cost, latency, and loop statistics from the Agent/Task Graph, without auto-applying any anomaly.

#### Scenario: Per-agent stats derived
- **WHEN** an operator views agent analytics
- **THEN** per-agent cost, latency, and loop statistics are available
- **AND** no anomaly is auto-applied
