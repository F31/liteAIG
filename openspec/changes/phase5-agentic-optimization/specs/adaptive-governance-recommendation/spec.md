# adaptive-governance-recommendation Specification (Delta)

## ADDED Requirements

### Requirement: Agent-level recommendations
Adaptive recommendations SHALL extend to agent-level routing (e.g., route to a cheaper endpoint) and remain suggest-only: accepting creates a Config Draft, never mutating production.

#### Scenario: Agent recommendation lands as a draft
- **WHEN** an agent-level routing recommendation is accepted
- **THEN** a Config Draft is created
- **AND** active configuration is unchanged until published
