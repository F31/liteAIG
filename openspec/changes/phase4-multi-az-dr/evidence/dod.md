# Phase 4 - Multi-AZ / Region DR: DoD Evidence

Status: Complete (10/10 tasks)

This change delivers the V8.2 Region DR foundation: Region-isolated Last Known
Good stores, node-and-bundle DR readiness coupling, and a deterministic,
read-only DR Runbook status view with budget consistency semantics.

## Capabilities Delivered

### Region-scoped LKG (`region-dr-layout`)
- `internal/platform/lkg.RegionStore` stores each Region under
  `regions/<region>/<tenant>/active.bundle` and reuses atomic active/previous
  rotation.
- A required `lkg.Verifier` gates boot readiness. `LoadVerified` tries active
  and then previous, so a readable but unverifiable active bundle can safely
  fall back without affecting another Region.
- `RegionReady` requires both node readiness and a verified tenant bundle.

### DR Runbook (`dr-runbook-checklist`)
- `internal/platform/dr.Checklist` runs validate-runtime, reconcile-accounting,
  reconcile-budget, switch-traffic, and verify-readiness in fixed order.
- Every recorded step includes pass/fail state and RTO/RPO hints; execution
  stops at the first failure and preserves the cause.
- `Checklist.View` exposes a read-only `Status` containing Region, tenant,
  readiness, steps, and the typed `regional/global_soft/global_hard` budget
  consistency mode.

## Gate Results

| Gate | Result |
| --- | --- |
| `go test -race -count=1 -timeout 180s ./...` (Postgres 55432 + Redis 56379) | PASS |
| `go vet ./...` | PASS |
| `go run ./cmd/architecture-test` | PASS |
| `actionlint` | PASS |
| `gitleaks --config .gitleaks.toml detect --no-banner --redact` | no leaks found |
| `npm run check` (`web/console`) | PASS |
| `openspec validate --all --strict` | 23 passed, 0 failed |

The first post-change full race run encountered the known shared-Postgres-schema
flake in `TestDataPlaneSnapshotSurvivesPostgresOutage`. The chaos package passed
alone, and the immediately following complete uncached race run passed.

## Tests

- Region isolation, corrupt-active fallback, unverifiable-active fallback,
  invalid/no-bundle readiness, and node-not-ready coupling.
- Runbook ordering, fail-fast recording, read-only execution, readiness view,
  budget consistency mode, and RTO/RPO hints on failures.
- Scenario G combines Region LKG isolation, the wired DR status view, and a
  failed accounting reconciliation that halts the runbook.

## Scope Note

This slice provides importable Region storage/readiness and DR drill primitives.
The verifier is required and is supplied by deployment wiring (for example the
existing Ed25519 RuntimeBundle verifier). Real Kubernetes/Helm orchestration,
multi-cluster traffic switching, KMS-backed keys, and automated failover remain
explicitly out of scope.
