# groundedness-check Specification

## ADDED Requirements

### Requirement: Response groundedness verdict
The system SHALL evaluate a response against the provided context (messages/document provenance) and return a Groundedness verdict (pass/fail) with evidence. A deterministic baseline SHALL be available that requires no external model; a replaceable model judge MAY back the same contract.

#### Scenario: Contradicting response flagged
- **WHEN** a response contradicts the provided context
- **THEN** the Groundedness verdict is fail with attributable evidence
- **AND** the configured action (block/mark/retroactive) is applied

#### Scenario: Baseline needs no external model
- **WHEN** the default OverlapChecker is used
- **THEN** it returns a verdict without any provider call
- **AND** external-judge latency stays zero for the baseline

### Requirement: Groundedness failures emit redacted events
A failing Groundedness verdict SHALL emit a Security Event with redacted evidence (content hashes, not raw response/context bodies).

#### Scenario: Redacted groundedness event
- **WHEN** a groundedness failure is recorded
- **THEN** the event carries hashes and a reason, not raw bodies
