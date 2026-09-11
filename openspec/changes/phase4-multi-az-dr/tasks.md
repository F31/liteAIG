## 1. Region-scoped LKG Layout

- [x] 1.1 Add `internal/platform/lkg/region.go`: a Region-scoped store wrapper (`data/regions/<region>/<tenant>/...`) reusing the atomic-write/previous-fallback `lkg.Store`.
- [x] 1.2 Add `RegionReady(ctx, region, tenant)` = node ready AND valid active bundle for the tenant.
- [x] 1.3 Add tests: region isolation (corrupt bundle in one Region doesn't affect another), DR readiness false on invalid bundle.

## 2. DR Runbook Checklist

- [x] 2.1 Add `internal/platform/dr`: `Checklist` with ordered steps (`ValidateRuntime`, `ReconcileAccounting`, `ReconcileBudget`, `SwitchTraffic`, `VerifyReadiness`), each producing a status + RTO/RPO hints.
- [x] 2.2 Add `Run(ctx, env)` that executes steps in order and halts (fail-fast) on the first failure, recording the runbook result (read-only).
- [x] 2.3 Add tests: ordered execution, fail-fast halt with recorded step, read-only runbook, budget-mode-aware DR view.

## 3. Wiring and Release Gates

- [x] 3.1 Wire the DR readiness + runbook status view (read-only), reflecting each Region's budget consistency mode.
- [x] 3.2 Add a Scenario G strengthening assertion (Region LKG isolation + DR runbook fail-fast).
- [x] 3.3 Run the full gate set (`go test -race ./...`, `go vet ./...`, architecture-test, `npm run check`, actionlint, gitleaks) and resolve blocking failures.
- [x] 3.4 Run `openspec validate --all --strict` and record Phase 4 Multi-AZ/DR DoD evidence.
