# Phase 4 — Global Soft Budget Slice: DoD Evidence

Status: Complete (10/10 tasks)

This change brings the V8.2 §2.8#12 cross-Region Budget consistency choice
(`regional` / `global_soft` / `global_hard`) to Budget enforcement, with
per-Region slices, bounded overshoot, and reconcile for `global_soft`.

## Capabilities Delivered

### Budget Consistency Mode
- `runtime.BudgetPolicy` and config `BudgetPolicy` gain `Consistency`
  (`regional|global_soft|global_hard`), default `regional`, compiled into the
  Tenant Runtime Snapshot with validator enforcement.

### Slice Authority (`global-soft-budget-slice`)
- `internal/platform/coordination/budgetslice`: `SliceAuthority` with per-Region
  slices; `Admit` is strict for `regional`/`global_hard` and bounded-overshoot
  for `global_soft`; `Reconcile` adjusts slices idempotently and emits a
  reconcile event; `ForMode` selects the authority by consistency mode.

### Budget Console
- `/api/admin/runtime` and `/api/admin/governance` expose read-only compiled
  Budget Policies, including their consistency mode.
- The localized Governance Budget view shows scope, window, limit, enforcement,
  and `regional/global_soft/global_hard` semantics. Configuration remains
  server-authoritative and cannot be changed from this view.

## Gate Results

| Gate | Result |
| --- | --- |
| `go test -race -timeout 180s ./...` (Postgres 55432 + Redis 56379) | PASS |
| `go vet ./...` | PASS |
| `go run ./cmd/architecture-test` | PASS |
| `actionlint` | PASS |
| `gitleaks --config .gitleaks.toml detect` | no leaks found |
| `npm run check` | PASS (7 tests, locale/string checks, production build) |
| `openspec validate --all --strict` | 24 passed, 0 failed |

## Tests

- Config/validator: default regional preserved, invalid mode rejected.
- Slice authority: regional strict, global_soft bounded overshoot admitted +
  reconcile (idempotent), overshoot beyond factor rejected, global_hard strict,
  `ForMode` selects the right authority.
- Admin/Console: compiled consistency is included in runtime resources; locale
  parity, hardcoded-string checks, component tests, and production build pass.

## Scope Note

The Budget view is read-only by design. Policy changes continue through the
server-side Config Draft/Validate/Diff/Publish lifecycle.
