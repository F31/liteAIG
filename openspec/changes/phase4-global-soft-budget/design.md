# Phase 4 — Global Soft Budget Slice: Design

## Context

Phase 1's `coordination.BudgetLedger` enforces the multi-window atomic
Reservation Ledger with home-instance authority (effectively `regional`). The
federation task counters (`internal/federation/task.go`) already model
`regional/global_soft/global_hard`. This change brings that same consistency
choice to Budget enforcement: a compiled Budget Policy carries a `ConsistencyMode`,
and `global_soft` uses per-Region slices with bounded overshoot then reconcile.

## Goals / Non-Goals

**Goals:**
- Budget Policies carry an explicit `regional/global_soft/global_hard` mode.
- A Budget slice authority where each Region owns a slice; `global_soft`
  permits bounded overshoot before reconcile; `regional`/`global_hard` are
  strict.
- Reconcile after overshoot with an emitted event; overshoot is bounded and
  never silent.

**Non-Goals:**
- OIDC/SAML/SCIM, ClickHouse sink, Multi-AZ/DR orchestration, gRPC Extension
  Bridge, Phase 5.

## Decisions

### 1. Consistency mode is compiled into the Budget Policy
Extend `runtime.BudgetPolicy` with `Consistency` (`regional|global_soft|
global_hard`), compiled from config with `regional` as the explicit default.
`regional` uses the existing home-instance ledger unchanged.
Alternative considered: deriving mode from deployment topology. Rejected — the
mode is a tenant policy decision (V8.2 §2.8#12), not inferred.

### 2. A slice authority wraps the ledger
New `internal/platform/coordination/budgetslice`: `SliceAuthority` owns
per-Region slice counters over the same window. `global_soft` admits up to
`SliceLimit = limit + overshoot` (e.g., +10%) on a Region's slice, then requires
reconcile; `regional` and `global_hard` use the strict window. `global_hard`
optionally funnels through a single strong-consistent ledger.
Alternative considered: single global counter always. Rejected — regional stays
cheap; global_hard is opt-in with RTT cost.

### 3. Overshoot is bounded, reconciled, and visible
When a `global_soft` slice exceeds its strict share, the authority records the
overshoot and emits a reconcile event; reconcile adjusts slices to the true
window. Overshoot is bounded by the configured factor and never silent (event +
metric).

## Risks / Trade-offs

- [Overshoot on regional] → regional and global_hard are strict; only global_soft overshoots by a bounded factor.
- [Reconcile race] → reconcile is idempotent on the slice authority; events are emitted once.
- [global_hard latency] → opt-in; documented RTT/availability trade-off.

## Migration Plan

1. Extend Budget Policy config/validator/snapshot with `Consistency` (default regional).
2. Add `internal/platform/coordination/budgetslice` (slice authority, admit/reconcile, events) with tests.
3. Wire the mode into the budget manager path (regional passthrough default).
4. Expose the mode in the Budget console view (localized).
5. Run full gate set + OpenSpec strict validation + record DoD evidence.

Rollback: default `regional` preserves the Phase 1 behavior; slice authority is
opt-in for `global_soft`.
