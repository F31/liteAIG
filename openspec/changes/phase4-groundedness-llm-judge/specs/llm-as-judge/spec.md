# llm-as-judge Specification

## ADDED Requirements

### Requirement: Rubric-based judge verdict
The system SHALL provide a replaceable model Judge that evaluates a response against a rubric and returns a verdict (pass/fail), a reason, and a score. A deterministic mock SHALL be available for tests; an OpenAI-compatible provider MAY implement the same contract.

#### Scenario: Judge returns verdict with reason
- **WHEN** a response is judged against a rubric
- **THEN** the result carries pass/fail, a reason, and a score
- **AND** a failing verdict applies the configured action

#### Scenario: Judge failure is off the TTFT path
- **WHEN** an external judge runs
- **THEN** its latency is recorded separately from Core overhead
- **AND** the mock judge costs ~0 latency
