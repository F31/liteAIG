# analytics-sink Specification

## ADDED Requirements

### Requirement: Buffered analytics sink
The system SHALL provide a replaceable Analytics Sink that buffers redacted analytics events and flushes them in batches on batch size or interval, writing to a ClickHouse-compatible HTTP insert endpoint. Business modules SHALL keep emitting standard Domain Events and SHALL NOT depend on the concrete sink.

#### Scenario: Batch flush on size
- **WHEN** buffered events reach the configured batch size
- **THEN** they are flushed to the sink in one batch

#### Scenario: Modules stay decoupled
- **WHEN** a business module emits an event
- **THEN** it emits a standard Domain Event
- **AND** never imports the ClickHouse sink

### Requirement: Retry, backpressure, and bounded memory
A failed flush SHALL be retried with bounded backoff; under a bounded backlog the sink SHALL drop the oldest events and record the drop. The sink SHALL never grow unboundedly.

#### Scenario: Failure retried with backoff
- **WHEN** a flush fails
- **THEN** it is retried with bounded backoff
- **AND** the backlog is bounded

#### Scenario: Pressure drops oldest
- **WHEN** the backlog exceeds its bound
- **THEN** the oldest events are dropped and recorded

### Requirement: Idempotent batches and redaction
Each analytics event SHALL carry an id so a retried batch does not duplicate rows. Analytics payloads SHALL exclude prompt/response bodies and secrets by construction.

#### Scenario: Retried batch is idempotent
- **WHEN** a batch is retried after a partial failure
- **THEN** acknowledged events are not re-sent
- **AND** stable ids prevent duplicates

#### Scenario: Payload is redacted
- **WHEN** an analytics payload is inspected
- **THEN** it contains no prompt/response body or secret
