# console-interaction-completeness

## Why

Changes 1–2 made the Lite management plane runnable and wired the backend data links (real pipeline, Live Tail, Governance, Projects). The Console still has several interaction gaps that prevent "every page can perform every management operation":

- **Dashboard** renders static zeros; there is no aggregate endpoint.
- **Config** requires manually typing a draft ID; there is no draft list, editor, or save path (only diff/publish/rebase).
- **vite dev** has no proxy, so `npm run dev` cannot reach `/api/admin` — the only way to exercise the console is the embedded build.
- **Resources / Projects** has no create form, so the create API from Change 2 is unreachable from the UI.
- Cache/Budget/Guardrail are read-only in the UI; per §37.36 they must be edited through the Config Draft lifecycle (no direct toggles).

This change closes those gaps so every page can drive its backend.

## What Changes

- **Dashboard aggregate endpoint**: `GET /api/admin/dashboard` returns request counts, token totals, average latency, and active config version, aggregated server-side (§37.49/50 — browser must not scan the ledger).
- **Draft list + editor**: `GET /api/admin/config/drafts` lists drafts for the tenant; the Config page gains a draft list, JSON editor, save (via existing `PUT .../drafts/{id}`), diff, publish, rollback, and versions.
- **vite dev proxy**: proxy `/api/admin` → the Lite admin address during `npm run dev`; add a `Makefile` `dev` target that builds/launches the backend and console together.
- **Project Center**: the Resources page's Projects tab gains a create form (POST /api/admin/projects) plus richer columns (regions, enforcement).
- **Cache/Budget/Guardrail editing**: exposed through the Config draft JSON editor (the compliant lifecycle), with secrets masked in diff/editor feedback.

## Capabilities

### New Capabilities
- `console-interaction-completeness`: Dashboard live stats; Config draft list + editor + save; Projects create form; dev proxy + one-shot local startup.

### Modified Capabilities
- `runnable-management-plane` / `browser-management-data-links`: Console pages reach all corresponding backend operations.

## Impact

- **Backend**: `GET /api/admin/dashboard` + `GET /api/admin/config/drafts` endpoints; `Backend` interface + `ControlBackend`/`liteBackend` implementations; `config.Service` drafts listing.
- **APIs/Console**: Dashboard, Config (draft list + editor + save), Resources (project create form); zh-CN/en-US locale updates.
- **Dependencies**: none new.
- **Tests**: dashboard aggregate, drafts list, projects create form (API), vite proxy config, locale/string checks, existing gates green.

### Non-Goals
- Real upstream connectors, PostgreSQL composition, persistent approval store, cross-process Live Tail (unchanged follow-ups).
