# Phase 4 — Global Soft Budget Slice

## Why

V8.2 §2.8#12 forbids "no global strong consistency by default": cross-Region Budget needs an explicit `regional` / `global_soft` / `global_hard` semantic. Phase 1 delivered the atomic multi-window Reservation Ledger (regional, home-instance authoritative) and the federation task counters already model regional/global_soft/global_hard; this change brings the same consistency choice to Budget enforcement so a multi-Region deployment can bound overshoot while reconciling, without forcing a global strong-consistent authority on every request.

## What Changes

- Add a `ConsistencyMode` to compiled Budget Policies: `regional` (home Region authoritative, the Phase 1 default), `global_soft` (per-Region counter slices with bounded overshoot then reconcile), and `global_hard` (single strong-consistent authority).
- Add a Budget slice authority: each Region owns a slice of the window; `global_soft` permits a bounded overshoot slice before reconcile, `regional`/`global_hard` are strict.
- Add reconcile: after a slice overshoots, the authority reconciles slices to the true window and emits an event; overshoot is bounded and never silent.
- Compile the mode into the Tenant Runtime Snapshot Budget Policy and expose it in the console Budget view.

## Capabilities

### New Capabilities
- `global-soft-budget-slice`: cross-Region Budget consistency modes with per-Region slices, bounded overshoot, and reconcile.

### Modified Capabilities
- `distributed-budget-ledger`: Budget Policies gain an explicit `regional/global_soft/global_hard` consistency mode (regional remains the default).

## Impact

- **Backend**: new `internal/platform/coordination` slice authority + `internal/finops/budget` mode plumbing; budget policy config/validator/snapshot extension; reconcile event.
- **APIs/Console**: Budget view shows the consistency mode (localized).
- **Dependencies**: no new third-party runtime dependency.
- **Tests**: regional strict, global_soft bounded overshoot + reconcile, global_hard strict single authority, mode compiled into snapshot, default preserved.

### Non-Goals
- OIDC/SAML/SCIM, ClickHouse sink, Multi-AZ/DR orchestration, gRPC Extension Bridge, Phase 5 (separate remainder items).

**Golden Scenario:** strengthens Scenario G (budget semantics during cross-Region operation) and Scenario B (cost/budget behavior under multi-instance load).
