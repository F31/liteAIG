# guardrail-streaming-shadow Specification

## ADDED Requirements

### Requirement: Layer 3 async shadow guard
Released stream content SHALL be sent to an External Guardrail asynchronously and MUST NOT enter the TTFT path. If a violation returns while the stream is still active, further output SHALL be terminated. Already-emitted content that cannot be retracted SHALL be recorded as a `BLOCK_RETROACTIVE` disposition with post-hoc handling.

#### Scenario: Retroactive block on late violation
- **WHEN** an async shadow guard detects a violation after content was released
- **THEN** the stream stops producing further output if still active
- **AND** a Security Event records `BLOCK_RETROACTIVE` for the already-released content

#### Scenario: Shadow guard is off the hot path
- **WHEN** a stream is running
- **THEN** the shadow evaluation never blocks chunk delivery to the client
