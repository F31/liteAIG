# a2a-adapter Specification

## ADDED Requirements

### Requirement: A2A 1.0 wire adapter
The system SHALL provide an A2A 1.0 adapter (Agent Card discovery, Task/Message/Artifact, optional JWS-signed Agent Card) behind the existing `Invoker` contract. The adapter SHALL only normalize wire traffic and connect; authorization, guardrails, budgets, and task governance SHALL execute in the pipeline.

#### Scenario: Agent card discovery is normalized
- **WHEN** an A2A Agent Card is discovered
- **THEN** it becomes a candidate resource (not a trusted target)
- **AND** no relationship is created automatically

#### Scenario: A2A invocation flows through governance
- **WHEN** an admitted task targets an external agent
- **THEN** the invocation passes through trust, grant, budget, guardrail, and task governance before reaching the connector

### Requirement: Material change review
Changes to an agent's endpoint, auth scheme, capability set, publisher, trust key, data boundary, or pricing SHALL place the relationship into `pending_review` and MUST NOT auto-activate. Only a review that re-verifies trust SHALL re-activate the relationship.

#### Scenario: Capability change requires review
- **WHEN** a partner Agent Card adds a new capability (e.g., `payment.execute`)
- **THEN** the relationship transitions to `pending_review`
- **AND** the new capability is not available until reviewed
