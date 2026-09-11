## ADDED Requirements

### Requirement: Atomic multi-window reservation ledger
Standard/Enterprise SHALL enforce Token and cost Budget through a Redis/Valkey atomic Reservation Ledger. A single Lua script MUST evaluate all Tenant, Project, and Key windows for a request; if any window would be exceeded, it MUST reject without modifying any counter. All budget keys of one Tenant MUST share a Redis Cluster hash tag.

#### Scenario: Hard budget blocks any window
- **WHEN** a request would exceed the Tenant, Project, or Key window limit in any active window
- **THEN** the Lua ledger rejects the reservation atomically
- **AND** no counter in any window is changed

#### Scenario: Atomic multi-window success
- **WHEN** a request fits every active window
- **THEN** all window counters are incremented, the Reservation Hash is written, and the reservation expiry entry is added in one atomic script

### Requirement: Idempotent reconcile and release
Reconcile SHALL apply the actual-minus-estimated delta to every reserved window exactly once using an idempotent Lua script. A reservation that was already finalized MUST NOT be reconciled again. Failed or cancelled requests without billable usage SHALL release the full estimate.

#### Scenario: Duplicate reconcile signal
- **WHEN** Reconcile is invoked twice with the same reservation
- **THEN** the second invocation reports ALREADY_FINALIZED
- **AND** window counters are adjusted only once

#### Scenario: Release on cancellation
- **WHEN** a request is cancelled before producing billable usage
- **THEN** the full estimated reservation is released from every window

### Requirement: Reservation sweeper
A Control Plane job SHALL sweep expired reservations every 10 seconds by default using the expiry ZSET, release still-reserved estimates, mark them expired, and remove the expiry entry. Reconciled or released entries SHALL only remove their expiry entry. The sweeper MUST remain correct even if a Gateway process is killed before asynchronous PostgreSQL recovery records are written.

#### Scenario: Sweeper recovers an abandoned reservation
- **WHEN** a reservation is abandoned and its expiry passes
- **THEN** the sweeper releases its estimate and removes the expiry entry
- **AND** the reservation transitions to expired

### Requirement: Redis-unavailable fail modes
Hard Budget policies SHALL fail closed when the ledger is unavailable; soft policies SHALL fail open with an alert. Lite SHALL continue using SQLite transactions or local counters. The behavior MUST be declared per Project and never default silently.

#### Scenario: Hard budget during Redis outage
- **WHEN** a hard-budget Project attempts a reservation while the ledger is unavailable
- **THEN** the request is rejected before resolution
- **AND** an alert records the fail-closed event

#### Scenario: Soft budget during Redis outage
- **WHEN** a soft-budget Project attempts a reservation while the ledger is unavailable
- **THEN** the request proceeds
- **AND** an alert records the fail-open event

### Requirement: Audit and recovery records
PostgreSQL SHALL record reservations, reconciliations, and releases for audit and recovery assistance. These records MUST NOT participate in admission-time correctness.

#### Scenario: Ledger reconcile is recorded
- **WHEN** a reservation is reconciled
- **THEN** a PostgreSQL recovery record is written asynchronously without delaying the ledger operation
