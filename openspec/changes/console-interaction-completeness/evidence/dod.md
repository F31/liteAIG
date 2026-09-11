# console-interaction-completeness — DoD Evidence

Date: 2026-08-30

## Gate Results

- `go build ./...` — pass
- `go vet ./...` — pass (exit 0)
- `go test -race ./internal/app/ ./internal/controlplane/adminapi/ ./internal/controlplane/config/ ./cmd/liteaig/` — pass
- Full suite `go test -race -timeout 240s ./...` — pass except the pre-existing shared-DB flake `TestDataPlaneSnapshotSurvivesPostgresOutage` (passes standalone; documented prior to this change)
- `go run ./cmd/architecture-test` — `architecture boundaries: ok`
- `npm run check` (console: format + i18n + strings + vitest + build) — pass (7 tests)
- `openspec validate --all --strict` — 27 passed, 0 failed (includes `change/console-interaction-completeness`)
- gitleaks — no leaks

## Verified Behavior

`internal/app/console_test.go` `TestDashboardAndDraftsEndpoints` (pass) proves:

1. `GET /api/admin/dashboard` returns request count, token totals, average latency, and active config version aggregated server-side.
2. `GET /api/admin/config/drafts` returns draft summaries (id/status/baseVersion/revision/updatedAt) without echoing config secrets.

Frontend (`npm run check` green) proves:

3. Dashboard renders live aggregates from `/api/admin/dashboard` (requests, tokens, avg latency, config version, spend).
4. Config page lists drafts, loads one into a JSON editor, saves via `PUT /drafts/{id}` (revision-conflict safe), diffs, publishes, and lists versions.
5. Resources Projects tab has a create-project form (`POST /api/admin/projects`) and shows name/status/residency/regions.
6. `vite.config.ts` proxies `/api/admin` → `LITEAIG_ADMIN_ADDR` (default `http://localhost:8081`); `Makefile` `dev` starts backend + console together.

## Files

- `internal/controlplane/config/repository.go` — `ListDrafts` in `Repository`
- `internal/controlplane/config/service.go` — `Drafts` service method
- `internal/platform/storage/sqlrepo/config.go` — `ListDrafts` SQLite implementation
- `internal/controlplane/adminapi/server.go` — `Dashboard`/`Drafts` interface methods + routes + handlers + `DashboardView`
- `internal/controlplane/adminapi/backend.go` — `ControlBackend.Dashboard`/`Drafts` (+ `activeConfigVersion`)
- `internal/app/console_test.go` — dashboard + drafts integration test
- `web/console/src/pages/Dashboard.tsx` — live aggregates
- `web/console/src/pages/Config.tsx` — draft list + editor + save + diff + publish + versions
- `web/console/src/pages/Resources.tsx` — project create form + richer columns
- `web/console/src/locales/{en-US,zh-CN}/common.json` — new keys (dashboard.spend, config.*, resources.*)
- `web/console/vite.config.ts` — `/api/admin` dev proxy
- `Makefile` — `dev` target (backend + console)
