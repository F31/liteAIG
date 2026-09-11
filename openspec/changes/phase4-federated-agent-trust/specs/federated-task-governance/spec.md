# federated-task-governance Specification

## ADDED Requirements

### Requirement: Sticky crosses_trust_boundary
A Task SHALL record whether it has ever crossed a trust boundary. Once `crosses_trust_boundary` becomes true, it SHALL NOT revert to false, because the task performed cross-organization data flows that must remain auditable.

#### Scenario: Boundary crossing is sticky
- **WHEN** a task invokes an external federated agent
- **THEN** `crosses_trust_boundary` is set to true for the task
- **AND** later internal-only hops do not reset it

### Requirement: Task hop, call, and cost counters
Task governance SHALL enforce `max_agent_hops`, `max_agent_calls`, and `max_total_cost` using a counter authority with an explicit consistency mode: `regional` (home region authoritative), `global_soft` (counter slices with bounded overshoot then reconcile), or `global_hard` (single strong-consistent authority). Exceeding any limit SHALL block the next downstream call.

#### Scenario: Hop limit blocks next call
- **WHEN** a task reaches `max_agent_hops`
- **THEN** the next downstream call is blocked before invocation
- **AND** the block is attributable to the counter

#### Scenario: Global hard mode is consistent
- **WHEN** a task uses `global_hard` consistency
- **THEN** counters are enforced against a single strong-consistent authority
- **AND** cross-region RTT is an accepted trade-off

### Requirement: Loop detection
A task SHALL detect agent call loops and terminate an anomalous chain; the loop evidence SHALL be attributable to the task graph.

#### Scenario: Loop terminated
- **WHEN** an agent calls itself transitively
- **THEN** the loop is detected and further hops are blocked
- **AND** a Security Event records the loop
