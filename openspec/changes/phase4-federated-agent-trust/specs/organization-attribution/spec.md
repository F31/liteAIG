# organization-attribution Specification (Delta)

## ADDED Requirements

### Requirement: External agent response provenance
External agent responses SHALL carry Content Provenance of type `external_agent_response`, distinct from `external_document`, and SHALL re-enter the Guardrail pipeline as untrusted content before being used as context or returned.

#### Scenario: External response is untrusted
- **WHEN** an external agent returns a response
- **THEN** it is marked as `external_agent_response` provenance
- **AND** it is re-evaluated by the applicable Guardrail before use
