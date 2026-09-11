# commercial-guardrail Specification (Delta)

## ADDED Requirements

### Requirement: Optional groundedness and judge checkpoints
The Output Guardrail stage SHALL support optional Groundedness and LLM-as-Judge checkpoints after the deterministic rules, with a configurable action (block / mark / retroactive) and redacted Security Events. Without configuration the stage SHALL behave exactly as before.

#### Scenario: Configured checkpoint blocks
- **WHEN** a Groundedness or Judge verdict fails with action `block`
- **THEN** the response is rejected before release
- **AND** a redacted Security Event is emitted

#### Scenario: Unconfigured stage unchanged
- **WHEN** no Groundedness or Judge is configured
- **THEN** the Output Guardrail stage behaves exactly as before
