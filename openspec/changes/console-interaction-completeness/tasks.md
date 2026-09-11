## 1. Dashboard Aggregate Endpoint

- [x] 1.1 Add `GET /api/admin/dashboard` to the admin `Backend` interface + `ControlBackend` implementation aggregating requests, tokens, average latency, and active config version from the accounting repository and config service.
- [x] 1.2 Add an integration test for the dashboard endpoint.

## 2. Config Draft List + Editor

- [x] 2.1 Add `GET /api/admin/config/drafts` (tenant draft list) via the config service/repository; add a `Drafts` surface to the admin `Backend` interface and implementations.
- [x] 2.2 Update the Config page: draft list select, JSON editor, save (PUT), diff, publish, rollback, versions; keep secret masking.
- [x] 2.3 Add an integration test for draft list + save + publish.

## 3. Dev Proxy + One-Shot Startup

- [x] 3.1 Add `/api/admin` proxy to `vite.config.ts` (target from `LITEAIG_ADMIN_ADDR`, default `http://localhost:8081`).
- [x] 3.2 Add a `Makefile` `dev` target that launches the Lite backend and the Console dev server together.

## 4. Project Center

- [x] 4.1 Add a create-project form to the Resources Projects tab (POST /api/admin/projects) and richer columns (regions, enforcement).

## 5. Release Gates

- [x] 5.1 Run the full gate set (`go test -race ./...`, `go vet ./...`, architecture-test, `npm run check`, actionlint, gitleaks) and resolve blocking failures.
- [x] 5.2 Run `openspec validate --all --strict` and record DoD evidence.
