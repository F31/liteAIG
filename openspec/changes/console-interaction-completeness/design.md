# console-interaction-completeness: Design

## Context

Changes 1–2 made the Lite management plane runnable and wired the backend data links (real pipeline, Live Tail, Governance, Projects). The Console still had interaction gaps: Dashboard rendered static zeros, Config required typing a draft ID by hand, vite dev had no `/api/admin` proxy, Projects had no create form, and Cache/Budget/Guardrail were read-only.

This change closes those gaps: server-aggregated Dashboard metrics, a Config draft list + editor + save + publish loop, a dev proxy + one-shot startup, and a Project Center create form.

## Goals / Non-Goals

**Goals:**
- `GET /api/admin/dashboard` returns server-aggregated request/token/latency/config-version metrics (§37.49/50).
- `GET /api/admin/config/drafts` lists drafts; the Config page selects a draft, edits its JSON, saves (PUT, revision-conflict-safe), diffs, publishes, and lists versions.
- vite dev proxies `/api/admin` to the Lite admin address; `make dev` starts backend + console together.
- Resources page creates projects and shows name/status/residency/regions.

**Non-Goals:**
- Real upstream connectors, PostgreSQL composition, persistent approval store (unchanged follow-ups).

## Decisions

### 1. Dashboard aggregate is server-side
`ControlBackend.Dashboard` reads `accounting.ListRequests` (bounded) + the active config version from the registry and aggregates counts/tokens/average latency/spend. The browser never scans the ledger.

### 2. Draft list returns a summary, editor loads one draft
`ControlBackend.Drafts` returns id/status/baseVersion/revision/updatedAt only — never the full config (secrets). The Config page loads `GET /drafts/{id}` into a JSON editor, PUTs `{revision, config}` to save (with `REVISION_CONFLICT` handling), diffs against the latest version, and publishes.

### 3. Dev proxy + one-shot startup
`vite.config.ts` proxies `/api/admin` to `LITEAIG_ADMIN_ADDR` (default `http://localhost:8081`) via `loadEnv`. `Makefile` `dev` target runs the Lite backend (`--db file:liteaig-dev.db`) and `npm run dev` together.

### 4. Cache/Budget/Guardrail editing stays on the draft lifecycle
The Config JSON editor exposes `cache`, `budget_policies`, and `guardrail` fields; edits flow through draft → diff → publish, honoring §37.36 (no direct toggles).

## Risks / Trade-offs

- [Draft list summary] → no secret echo in the list; the editor fetches one draft on demand.
- [Dev proxy default] → defaults to `localhost:8081`, overridable via env.

## Migration Plan

1. Backend: `ListDrafts` (config repo/service), `Dashboard` + `Drafts` admin endpoints.
2. Console: Dashboard wired; Config draft list + editor + save; Resources create-project form; locale updates.
3. vite proxy + Makefile `dev`.
4. Tests + gates; record evidence.
