## ADDED Requirements

### Requirement: ZSET concurrency lease
Concurrency leases SHALL be tracked in Redis/Valkey ZSET keys without full-key scans. Acquire SHALL remove expired members, check capacity, insert the lease with its expiry score, and set a bounded key TTL in a single Lua script with O(log N) complexity.

#### Scenario: Capacity exceeded
- **WHEN** the active lease count has reached the configured capacity
- **THEN** acquire returns a busy result without inserting a lease

#### Scenario: Expired leases do not block capacity
- **WHEN** a lease's score is in the past
- **THEN** the next acquire removes it before capacity evaluation

### Requirement: Lease renew
Long-running streaming requests SHALL renew their lease periodically before expiry. Renew SHALL update the score only when the lease still exists and MUST NOT create a missing lease.

#### Scenario: Stream renews its lease
- **WHEN** a stream renews an existing lease
- **THEN** the lease expiry is extended
- **AND** no new lease is created when the original is absent

### Requirement: Lease release and cleanup
Release SHALL remove the lease from the ZSET. A killed process MUST NOT permanently consume capacity; expired leases SHALL be cleared by the next acquire or a background cleanup job.

#### Scenario: Process killed without release
- **WHEN** a process dies without releasing a lease
- **THEN** capacity is recovered after expiry via acquire-time cleanup or the background job

### Requirement: Circuit-aware leasing
Deployments with an open circuit MUST NOT acquire a lease. Half-open probes SHALL still acquire a lease before invoking the provider.

#### Scenario: Open circuit skips leasing
- **WHEN** resolution filters a deployment because its circuit is open
- **THEN** no lease is acquired for that deployment
- **AND** a half-open probe still acquires capacity
