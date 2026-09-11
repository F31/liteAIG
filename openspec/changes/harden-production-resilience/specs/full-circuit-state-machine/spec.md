## ADDED Requirements

### Requirement: Complete circuit state machine
Circuits SHALL track full per-provider, per-deployment, and per-credential state with Closed, Open, and Half-Open transitions. The minimum sample size, error-rate threshold, initial cooldown, cooldown escalation, and probe semantics SHALL be configurable and compiled from the Tenant Snapshot.

#### Scenario: Cooldown escalation
- **WHEN** a circuit remains open across repeated failures
- **THEN** its cooldown escalates according to the configured schedule up to a configured maximum

### Requirement: Guarded half-open probes
Exactly one probe SHALL be admitted per Half-Open cooldown window for a given circuit tuple. A successful probe SHALL close the circuit and reset counters; a failed probe SHALL reopen it.

#### Scenario: Exclusive probe
- **WHEN** the circuit enters Half-Open
- **THEN** only one request is allowed to probe
- **AND** concurrent requests are rejected until the probe resolves

### Requirement: Transition events and drift reset
Every circuit transition SHALL emit a redacted operational event with the tuple, from-state, to-state, and timestamp. A configuration drift that changes circuit parameters SHALL reset the affected circuit state deterministically.

#### Scenario: Drift resets circuit
- **WHEN** circuit parameters change through publication
- **THEN** affected circuit tuples are reset
- **AND** the reset is emitted as a transition event

### Requirement: Distributed consistency
Phase 1 SHALL keep circuit decision metadata process-local for hot-path speed while persisting or replicating durable circuit facts in a way that survives restarts without over-counting probes across replicas.

#### Scenario: Restart preserves decisions
- **WHEN** a gateway restarts
- **THEN** it reconstructs circuit state from durable facts without double-counting in-flight probes
