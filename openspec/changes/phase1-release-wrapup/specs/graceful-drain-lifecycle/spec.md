# graceful-drain-lifecycle Specification

## ADDED Requirements

### Requirement: Signal-driven drain lifecycle
The runtime SHALL enter a `DRAINING` state on a termination signal (SIGTERM). While draining, the process SHALL report not-ready on `readyz`, stop accepting new requests, allow in-flight requests to complete, protect active long-lived streams, finalize usage/budget/lease for in-flight work, and exit within configured timeouts.

#### Scenario: SIGTERM drains before exit
- **WHEN** a process receives SIGTERM while serving traffic
- **THEN** it transitions to `DRAINING`, `readyz` reports not-ready, and load balancers stop sending new traffic
- **AND** in-flight requests complete before the process exits

#### Scenario: Drain timeout forces cancellation
- **WHEN** an in-flight request exceeds `drain_timeout`
- **THEN** the remaining upstream work is cancelled
- **AND** usage/budget/lease finalization still runs before exit

### Requirement: Streaming-aware shutdown
Active SSE, A2A, and MCP long streams SHALL be allowed to finish within `stream_drain_timeout` rather than being terminated at drain start. The runtime SHALL track active long streams and refuse to start new long streams while draining.

#### Scenario: Long stream completes during drain
- **WHEN** the process drains while an SSE or A2A stream is active
- **THEN** the stream continues to its natural end within `stream_drain_timeout`
- **AND** no new long stream is accepted after drain begins

### Requirement: Finalized accounting on drain
Budget reservations, concurrency leases, and usage records for in-flight requests SHALL be reconciled or released before process exit, matching the normal finalization semantics of the seven-stage pipeline.

#### Scenario: Lease released on drained exit
- **WHEN** a drained request held a concurrency lease and an unreconciled budget reservation
- **THEN** the lease is released and the reservation is reconciled or released before exit
- **AND** no capacity is left consumed after restart

### Requirement: Configurable drain timeouts
Drain behavior SHALL be governed by validated typed configuration: `drain_timeout`, `stream_drain_timeout`, `force_shutdown_timeout`, and `terminationGracePeriodSeconds` mapping. The runtime SHALL provide explicit defaults and validate the configuration.

#### Scenario: Configured drain window honored
- **WHEN** `drain_timeout` and `stream_drain_timeout` are configured
- **THEN** ordinary requests are bounded by `drain_timeout` and streams by `stream_drain_timeout`
- **AND** a hard `force_shutdown_timeout` caps the entire drain phase
