# distributed-budget-ledger Specification (Delta)

## ADDED Requirements

### Requirement: Consistency mode on budget policies
The distributed Budget Ledger SHALL honor a compiled `regional/global_soft/global_hard` consistency mode on Budget Policies. `regional` keeps the existing atomic ledger semantics; `global_soft` delegates to the slice authority with bounded overshoot; `global_hard` uses a single strong-consistent authority.

#### Scenario: Mode compiled into snapshot
- **WHEN** a Budget Policy is published with a consistency mode
- **THEN** the mode is compiled into the Tenant Runtime Snapshot
- **AND** enforcement honors it per the global-soft-budget-slice capability
