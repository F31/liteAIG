## 1. Budget Consistency Mode

- [x] 1.1 Extend `runtime.BudgetPolicy` and config `BudgetPolicy` with `Consistency` (`regional|global_soft|global_hard`), default `regional`.
- [x] 1.2 Wire config validator + compiler + snapshot so the mode is compiled and validated.
- [x] 1.3 Add tests: default regional preserved, mode compiled into snapshot, invalid mode rejected.

## 2. Slice Authority

- [x] 2.1 Add `internal/platform/coordination/budgetslice`: `SliceAuthority` with per-Region slices, `Admit(region, window)` for `global_soft` (bounded overshoot), and `regional`/`global_hard` strict paths.
- [x] 2.2 Add `Reconcile` that adjusts slices to the true window idempotently and emits a reconcile event.
- [x] 2.3 Add tests: regional strict, global_soft bounded overshoot admitted + reconcile, overshoot beyond factor rejected, global_hard strict single authority, reconcile idempotent.

## 3. Wiring and Release Gates

- [x] 3.1 Wire the mode into the budget manager path (regional passthrough default; slice authority for global_soft).
- [x] 3.2 Expose the consistency mode in the Budget console view (localized).
- [x] 3.3 Run the full gate set (`go test -race ./...`, `go vet ./...`, architecture-test, `npm run check`, actionlint, gitleaks) and resolve blocking failures.
- [x] 3.4 Run `openspec validate --all --strict` and record Phase 4 global-soft-budget DoD evidence.
