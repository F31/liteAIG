# close-runnable-management-plane — DoD Evidence

Date: 2026-08-30

## Gate Results

- `go build ./...` — pass
- `go vet ./...` — pass (exit 0)
- `go test -race ./internal/app/ ./cmd/liteaig/ ./internal/controlplane/adminapi/ ./internal/controlplane/setup/ ./internal/controlplane/config/` — pass
- Full suite `go test -race -timeout 180s ./...` — pass except the pre-existing shared-DB flake `TestDataPlaneSnapshotSurvivesPostgresOutage` (passes standalone; documented prior to this change)
- `go run ./cmd/architecture-test` — `architecture boundaries: ok`
- `npm run check` (console: format + i18n + strings + vitest + build) — pass
- `openspec validate --all --strict` — 25 passed, 0 failed (includes `change/close-runnable-management-plane`)
- gitleaks — no leaks (repo not a git checkout here, scan reports no leaks)

## Verified Behavior

`internal/app/lite_test.go` `TestLiteManagementPlaneEndToEnd` (pass) proves:

1. `POST /api/admin/setup` runs the full Lite wizard and returns `virtualKey`, `sdkExample`, `requestId`, `tenantId`, `projectId`; a second setup is rejected.
2. `POST /api/admin/session` with local credentials returns a session cookie + CSRF token.
3. `GET /api/admin/me` (authenticated) resolves the tenant scope.
4. `GET /api/admin/runtime` reflects providers, deployments, logical models, routes.
5. `GET /api/admin/requests` includes the Setup first-call request record.
6. `GET /dashboard` returns the Console SPA (embedded dist).

## Files

- `internal/app/lite.go` — Lite composition root + `liteBackend` + console handler + audit/security/tool-call read paths
- `internal/app/providersetup.go` — Lite mock `ProviderSetup` + `FirstCallRunner`
- `internal/app/playground.go` — Lite playground (records `source=playground`)
- `internal/app/clock.go`, `internal/app/pepper.go` — real clock + seeded key pepper
- `internal/app/lite_test.go` — end-to-end integration test
- `cmd/liteaig/main.go` — `--db` / `--admin-addr` flags wiring the management plane
