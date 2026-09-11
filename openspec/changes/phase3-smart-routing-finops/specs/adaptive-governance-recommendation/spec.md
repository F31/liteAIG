# adaptive-governance-recommendation Specification

## ADDED Requirements

### Requirement: Suggest-only adaptive recommendations
The system SHALL generate routing and guardrail optimization recommendations with an evidence window and explanation. Recommendations MUST NOT modify production configuration directly; accepting a recommendation SHALL create a Config Draft through the normal Draft/Publish path.

#### Scenario: Accept creates a draft
- **WHEN** an operator accepts a routing recommendation
- **THEN** a Config Draft is created with the proposed change
- **AND** no active configuration is mutated until the draft is validated and published

#### Scenario: Dismiss leaves production unchanged
- **WHEN** an operator dismisses a recommendation
- **THEN** active configuration is unchanged
- **AND** the dismissal is recorded

### Requirement: Evidence-bounded recommendations
Every recommendation SHALL carry the evidence window (time range, samples, and metric basis) that produced it, and SHALL NOT claim performance improvements outside that window.

#### Scenario: Recommendation has evidence window
- **WHEN** a recommendation is listed
- **THEN** its evidence window and basis are displayed
- **AND** no automatic application occurs
