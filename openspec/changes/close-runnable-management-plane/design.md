# close-runnable-management-plane: Design

## Context

`cmd/liteaig/main.go` previously only started the readiness server. `internal/app.Compose` returned a `Plan` but never constructed business HTTP services; `adminapi.New`, the gateway, and `console.Assets` had no callers. `ControlBackend` reported `errNotWired` for Setup/CreateKey/Playground/Projects/Audit. The result: no browser-accessible management surface.

This change adds a **Lite runnable management plane**: a single binary that opens SQLite, runs migrations, seeds a key pepper, composes all repositories and services, serves the Admin API + session endpoints + the embedded Console, and closes the Setup wizard.

## Goals / Non-Goals

**Goals:**
- `POST /api/admin/setup` completes the Lite wizard end to end (bootstrap → provider → key → first call) and returns one-time artifacts.
- `/api/admin/session` login + `DELETE /api/admin/session` + `GET /api/admin/oidc/config` (disabled) + `/api/admin/me`.
- Console SPA served from the embedded `dist` with a fallback to `index.html`; `/api/admin/*` routed to the Admin API.
- `cmd/liteaig` gains `--db` and `--admin-addr`; without `--db`, readiness-only behavior is preserved (existing `main_test` stays green).
- Runtime/Health/Requests/Usage read from the active registry + repositories.

**Non-Goals:**
- Gateway `/v1` data-plane server, real upstream provider invoker, Live Tail publishing, Governance write paths (approval/federation suspend/agent graph), PostgreSQL/Redis production composition. Those are later changes.

## Decisions

### 1. Composition root owns the Lite profile
`internal/app.NewLite(ctx, LiteOptions)` is the single assembly point: `sqlite.Open` → `migrations.Apply` → `ensureLocalPepper` → repositories → services (`apikey.Service`, `config.Service`, `alert.Service`, `setup.Service` bootstrap, `setup.Wizard`) → `adminapi.NewControlBackend` → `adminapi.New` + `SessionManager` + `SessionEndpoints`. Returns a `Lite` with `Handler()` and `Close()`.

### 2. Lite backend overrides the not-wired surfaces
`liteBackend` embeds `*adminapi.ControlBackend` and overrides `Setup` (wizard adapter), `CreateKey` (apikey.Service), `Playground` (lite playground recording via accounting repo), `Projects` (tenancy repo), `Audit` (audit_events table). Security events and tool calls are wired via sqlite stores.

### 3. Setup closure through the wizard
`setup.Wizard` requires bootstrap, `ProviderSetup`, `KeyCreator`, and `FirstCallRunner`. The Lite `liteProviderSetup` registers a mock provider and `ConfigureDefault` builds a complete `config.TenantConfig` (provider, credential, deployment, route policy, logical model) then creates + publishes a draft so the runtime snapshot activates. `liteFirstCall` records `source=playground` request facts via the accounting repository.

### 4. Routing and Console
A single root `http.ServeMux`: `"/api/admin/"` → `adminapi.Server`, session endpoints registered on the root mux, `"/"` → Console SPA with `index.html` fallback. The Console `dist` is embedded via `web/console`.

## Risks / Trade-offs

- [Secure cookie over plain HTTP] → `Secure: true` is required by `SessionManager`; Lite is intended for local/TLS deployment. Dev note: use HTTPS or set `--ready-addr`/`--admin-addr` behind a TLS proxy.
- [Mock provider] → explicitly the Lite baseline (user decision); real provider connectors remain for the Standard profile.
- [In-memory pepper] → regenerated per process; documented Lite trade-off (keys rotate on restart).

## Migration Plan

1. Add `internal/app` Lite composition root, mock provider setup, first-call runner, playground, console handler.
2. Wire `cmd/liteaig/main.go` flags.
3. Integration test: Setup → login → runtime → requests → console.
4. Run gates; record evidence.
