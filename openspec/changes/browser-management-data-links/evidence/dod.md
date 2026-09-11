# browser-management-data-links — DoD Evidence

Date: 2026-08-30

## Gate Results

- `go build ./...` — pass
- `go vet ./...` — pass (exit 0)
- `go test -race ./internal/app/ ./internal/controlplane/adminapi/ ./internal/controlplane/setup/ ./cmd/liteaig/` — pass
- Full suite `go test -race -timeout 240s ./...` — pass except the pre-existing shared-DB flake `TestDataPlaneSnapshotSurvivesPostgresOutage` (passes standalone; documented prior to this change)
- `go run ./cmd/architecture-test` — `architecture boundaries: ok`
- `npm run check` (console: format + i18n + strings + vitest + build) — pass
- `openspec validate --all --strict` — 26 passed, 0 failed (includes `change/browser-management-data-links`)
- gitleaks — no leaks (repo not a git checkout here)

## Verified Behavior

`internal/app/datalinks_test.go` (pass) proves:

1. `TestPlaygroundRunsProductionPipeline` — `POST /api/admin/playground` runs through the real seven-stage pipeline; the same `requestId` returns from `GET /api/admin/requests/{id}` with `routeEvidence`, `attempts`, and `success` outcome.
2. `TestGovernanceSurfacesReturnRealData` — `/api/admin/approvals` (empty inbox, no errNotWired), `/api/admin/agent-graph/{root}` (hops from ledger), `/api/admin/federation` (renders).
3. `TestLiveTailStreamsRealEvents` — opening `GET /api/admin/live` then triggering a playground request delivers an `event: request.summary` SSE line.
4. `TestProjectsCreate` — `POST /api/admin/projects` creates a project and it appears in `GET /api/admin/projects`.

`internal/app/lite_test.go` `TestLiteManagementPlaneEndToEnd` still passes (Setup → login → me → runtime → requests → console).

## Files

- `internal/app/pipeline.go` — real seven-stage pipeline (planner + executor + mock invoker + accounting + LiveBus)
- `internal/app/governance.go` — `memoryApprovalStore`, `liteApprovals`, `liteAgentGraph`, `liteFederation`
- `internal/app/playground.go` — Lite playground/first-call via `gateway/playground.Service`
- `internal/app/lite.go` — composition wiring (pipeline, governance, projects create, Federation overlay)
- `internal/app/datalinks_test.go` — integration tests for the new surfaces
- `internal/controlplane/adminapi/server.go` — `CreateProject` in `Backend` interface + `POST /api/admin/projects` route
- `internal/controlplane/adminapi/backend.go` — `ControlBackend.CreateProject` (errNotWired contract)
