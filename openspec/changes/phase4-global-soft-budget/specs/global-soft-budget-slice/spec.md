# global-soft-budget-slice Specification

## ADDED Requirements

### Requirement: Budget consistency modes
Budget Policies SHALL carry an explicit `regional` | `global_soft` | `global_hard` consistency mode compiled from configuration, with `regional` as the default. `regional` SHALL use the home-Region ledger unchanged; `global_hard` SHALL use a single strong-consistent authority; `global_soft` SHALL use per-Region slices with a bounded overshoot before reconcile.

#### Scenario: Regional is the default
- **WHEN** a Budget Policy has no explicit consistency mode
- **THEN** it behaves as `regional` with the home-instance ledger
- **AND** Phase 1 behavior is unchanged

#### Scenario: Global hard is strict
- **WHEN** a Budget Policy uses `global_hard`
- **THEN** enforcement uses a single strong-consistent authority
- **AND** cross-Region RTT is an accepted trade-off

### Requirement: Bounded overshoot and reconcile
A `global_soft` slice SHALL admit up to its strict share plus a bounded overshoot factor, then SHALL require reconcile. Overshoot SHALL be bounded, reconciled idempotently, and never silent (an event SHALL be emitted).

#### Scenario: Overshoot bounded and reconciled
- **WHEN** a `global_soft` slice exceeds its strict share within the overshoot factor
- **THEN** it is admitted
- **AND** a reconcile event is emitted and the slices are adjusted to the true window
- **AND** overshoot beyond the factor is rejected
