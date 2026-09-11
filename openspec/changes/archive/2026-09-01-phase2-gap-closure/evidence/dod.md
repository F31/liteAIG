# Phase 2 Gap Closure — DoD Evidence

Status: Complete

This change closed the "half-done" gaps from `phase2-commercial-guardrail-cache`:
durable guardrail policies, snapshot-driven runtime enforcement, streaming Layer 3
shadow guard, real cache/spool metric wiring, and queryable Security Events /
Tool Calls surfaces.

## Capabilities Delivered

### Guardrail Policy Persistence (`guardrail-policy-persistence`)
- `guardrail_policies` migration (owner guardrail, composite `(tenant_id, version)`
  PK, change_type/actor/published_at).
- `sqlrepo.GuardrailPolicyStore` with tenant-scoped Create/GetActive and
  cross-tenant isolation.
- `PolicyRegistry` persists via `PolicyStore` on Fast Publish and reloads the
  active policy on startup (`NewPolicyRegistryWithStore`).
- Active policy compiles into `TenantRuntimeSnapshot.GuardrailPolicy()`; the
  Stage 2/6 handler builds its engine from the captured snapshot policy, so
  Tighten/Loosen is effective at runtime without a full config republish and
  in-flight requests keep their captured version.

### Streaming Shadow Guard (`guardrail-streaming-shadow`)
- `ShadowGuard` evaluates already-released content against an External Provider
  asynchronously (never on the delivery path), closes a per-request stop channel
  on a late violation, and reports the disposition retroactively.

### Observability Wiring
- `observability.CacheMetricsAdapter` and `SpoolMetricsAdapter` forward
  `cache.Metrics` and `accounting.SpoolMetrics` to the `MetricSink` with
  low-cardinality names.

### Tool Call and Security Event Surfaces
- `tool_call_events` migration (owner observability) + `sqlrepo.ToolCallStore`.
- `GET /api/admin/security-events` and `GET /api/admin/tool-calls` (tenant-scoped).
- Governance Console renders real Security Event and Tool Call lists (zh-CN/en-US
  parity, no hard-coded strings).

### Runtime Assembly
- `TestPipelineAssemblyComposesAllStages` proves the seven fixed stages plus the
  Exact Cache and snapshot-driven guardrail handlers run a request end to end
  with the compile-time-fixed order and cache-hit semantics.

### HTTP Runtime Closure (2026-08-31)
- `cmd/liteaig` starts the real data-plane gateway alongside admin/readiness via
  `--gateway-addr`, and SDK examples point at that gateway base URL.
- `internal/gateway/server` exposes real protocol endpoints for OpenAI
  (`/v1/chat/completions`, `/v1/embeddings`, `/v1/models`), Anthropic
  (`/v1/messages`), MCP (`/mcp`), and A2A (`/a2a`).
- `internal/app` wires bearer API-key auth, active snapshot admission, the Lite
  governed pipeline, rate/budget preflight, budget reconciliation/release, cache
  writeback, circuit breaker, credential-pool rotation, real provider connectors,
  MCP tool governance/invocation, and A2A configured endpoint invocation.
- Real-binary SDK smoke booted a local OpenAI-compatible provider plus the
  `liteaig` binary with temp SQLite storage, ran setup over the Admin API to get
  a virtual key, then exercised the OpenAI and Anthropic SDKs against the real
  gateway (`LITEAIG_BASE_URL` + `LITEAIG_API_KEY`).

### Follow-up Closure (2026-08-31)
- `POST /api/admin/config/drafts` creates a new editable draft based on the latest
  published version (`config.Service.CreateDraft`), resolving the gap where the
  one-shot wizard draft is left `published` and can no longer be re-edited
  (`UpdateDraft` requires `status='editing'`). Returns `409 NO_PUBLISHED_VERSION`
  when no version exists to base a draft on.
- The Lite circuit breaker now honors the published per-tenant snapshot circuit
  config. `litePipeline.Run` syncs the breaker via `SetConfig` from
  `Snapshot.CircuitConfig()` on each request; `breakerConfigFromSnapshot` maps the
  compiled `runtime.CircuitConfig` to `circuit.Config` and keeps built-in defaults
  when a tenant never set circuit parameters. `circuit.Breaker.SetConfig`/`cfg()`
  make the parameters swap atomically (in-flight decisions keep the prior config).
- Real-binary SDK smoke extended to streaming chat, `/v1/embeddings`, MCP
  `tools/call` (upstream fixture), and A2A `message/send` (upstream fixture),
  gated by `LITEAIG_SMOKE_MCP` / `LITEAIG_SMOKE_A2A`. MCP/A2A persistence verified
  against temp SQLite: `tool_call_events` rows plus `request_records` with
  `source` attribution (`playground`/`gateway`/`mcp`/`a2a`).
- Standard distributed path validated end to end against live services: the
  Postgres repository conformance suite (tenant scoping, cross-tenant isolation,
  transactions) and the Redis coordination suite (namespace isolation, leases,
  ledger) ran 24 tests with 0 skips (and clean under `-race`) against
  Postgres `:55432` and Redis `:56379`. The Lite profile itself remains
  SQLite + in-memory coordination by design (single binary); no separate
  standard-profile binary is required for this change.

### Review Optimization Closure (2026-08-31)
Executed the full review optimization checklist (P0 → P1 → P2) to completion:

- **P0.1 startup reconcile** — `config.Service.ListLatestVersions`/`ReconcileAll`
  restore in-memory tenant runtimes from durable config at boot (best-effort per
  tenant, failure logged, startup not blocked); `readiness` signature takes the
  compiled snapshot; restart test proves data-plane traffic after a restart.
- **P0.2 readiness reflects runtime** — `/readyz` additionally requires
  `Lite.HasActiveRuntime()` in gateway mode, so a boot with zero restored
  tenants reports not-ready instead of serving 401s.
- **P0.3 accounting spool reliability** — spool rewritten with per-record
  checksums, torn-tail truncation, prefix-commit recovery, and O(1) stats;
  wired into the pipeline via `spoolAccounting` (hard/soft policy per tenant),
  background flusher, and restart replay; file DSNs only (in-memory stays
  direct).
- **P0.4 budget finalizer + lazy eviction** — settlement `settled` flag makes
  finalization idempotent per request (no double settlement/leak); budget and
  rate-limit state are evicted lazily on access instead of a global sweep.
- **P0.5 console fixes** — `useSSEStream` onEvent via ref (no stale closure, no
  re-subscribe loop) + `onStateChange` callback; `LiveTail` reports a real
  connected state (initial false, driven by SSE state); `MutationCache.onError`
  toasts API errors exactly once (local handlers win); `Governance` JSON.parse
  guarded with a localized error.
- **P0.6 egress/SSRF posture** — `platform/egress` (CIDR deny list for
  internal ranges, redirect blocking, body cap, no-redirect transport) is the
  single egress client for model, MCP, and A2A upstreams; loopback-listener
  test proves internal targets are refused.
- **P1.7 MCP/A2A governance** — non-model data-plane traffic now runs the same
  admission as model traffic: `admitTool` (rate policy + budget reservation)
  and `finalizeTool` (settle/release + live summary with protocol source);
  `invokeWithResilience` adds per-target circuit keys (`mcp-server:<id>`,
  `a2a-agent:<id>`) and one retry for retryable upstream errors; E2E proves
  retry-once, 502 on persistent failure, and accounting for both outcomes.
- **P1.8 guardrail FastPublish closed loop** — FastPublish order is now
  store → audit → memory (a store failure leaves memory untouched, with
  rollback test); optimistic version conflict on concurrent publish;
  `config.Service.OverlayGuardrail` activates the compiled policy on the data
  plane immediately; startup replays the stored active policy via
  `GuardrailLoader`; E2E proves publish → immediate enforcement → restart →
  still enforced → audit event recorded.
- **P1.9 performance batch** — rate limiter sharded (16 shards, FNV-1a) with
  lazy per-shard sweeps; OpenAI/Anthropic decoders parse request bytes in one
  pass (no string round-trip, no re-marshal); guardrail compiled-engine cache
  (bounded, content-hash keyed); SQLite WAL + `synchronous=NORMAL` and bounded
  pool for file DSNs; Postgres pool parameters; MCP/A2A connectors cached per
  URL in the gateway core.
- **P1.10 telemetry contract** — `kernel/contracts` gains `LiveEvent` +
  `TelemetrySink`; the Lite pipeline depends only on the contract (no
  `adminapi` import); `adminapi.LiveBus` adapts to it via `PublishLive`.
- **P1.11 drain wiring** — the drain lifecycle is now actually wired: both
  Lite handlers pass through `drainGate` (503 `DRAINING` once draining, every
  accepted request counted in-flight); `Lifecycle.Acquire` makes
  check-and-count atomic against `BeginDrain` (closes a WaitGroup race found
  by `-race`); long streams register in the `StreamRegistry` from
  `litePipeline.Run`; the accounting spool flush is the drain finalizer
  (shared lifecycle with `cmd/liteaig`, so readiness/drain observe one state
  machine). Tests: unit (gate waits for in-flight, refuses new) + Lite E2E
  (503 while draining, drain completes, not ready after).
- **P2 quick wins** — `estimateContextTokens` wired into the Lite planner so
  `ContextWindow` eligibility is enforced from real request size; repo-wide
  `gofmt` clean (CI enforces); federation `Relationship.Version` optimistic
  guard — stale copies can no longer clobber newer stored state
  (`TestStaleCopyCannotClobberStoredState`); RLS extended to every
  tenant-scoped table (added `tenant_memberships`, `alert_rules`, `alerts`,
  `guardrail_policies`, `tool_call_events`, `approval_requests`,
  `federation_relationships`, `secret_material` with the shared-NULL rule).

### Real-Binary Smoke (2026-08-31, post-optimization)
Built `cmd/liteaig` from the optimized tree and ran the full smoke
(`/tmp/opencode/smoke/smoke.sh`) **three consecutive times, 18/18 assertions
each time** against a file SQLite DSN (spool enabled) with local
OpenAI/MCP/A2A mock upstreams:

- data plane: `/v1/models`, non-stream chat, SSE streaming chat,
  `/v1/embeddings`, governed MCP `tools/call`, governed A2A `message/send`;
- guardrail fast-publish (`tighten`) blocks a keyword in chat with 400;
- accounting `request_records` carry `source` attribution `gateway`/`mcp`/`a2a`;
- `SIGTERM` → graceful drain → exit 0;
- restart on the same DB: chat works (runtime reconciled), MCP tool still
  configured, and the fast-published guardrail is still enforced (startup
  replay).

### Console CRUD Closure (2026-09-01)

Closed the operator-console usability gaps with visual, publish-safe workflows:

- Setup is preset-driven for common providers, defaults username/workspace, offers
  model autocomplete and password generation, and keeps only advanced settings
  behind an explicit toggle.
- `scripts/start-liteaig.sh` runs the Lite profile on configurable ready/admin/
  gateway addresses; local verification used `18080/18081/18082` because `8081`
  was occupied.
- Resources Console now supports draft-backed Provider, Deployment, Logical Model,
  and MCP Server CRUD. Edits update config drafts first and only affect runtime
  after Save + Publish, preserving the config lifecycle.
- Credential operations now have explicit Rotate and Disable actions. Rotate
  updates encrypted secret material; Disable publishes `credential.status=disabled`
  before disabling the vault ref.
- Virtual API Keys can be created from the Console (secret shown once) and revoked
  through `POST /api/admin/keys/{id}/revoke`.
- MCP Server discovery is exposed through
  `POST /api/admin/mcp-servers/{id}/discover`; discovered tools can be applied to
  the current draft as governed tool resources.
- Guardrail Fast Publish now has a visual rule editor (`id`, `kind`, `pattern`,
  `action`, optional `replacement`) instead of requiring raw JSON.

Verification for this closure:

| Gate | Result |
| --- | --- |
| `npm run check` after Resources/Governance changes | PASS |
| `go test ./internal/identity/apikey ./internal/platform/storage/sqlrepo ./internal/controlplane/adminapi ./internal/app ./internal/connectors/tool/mcp -count=1` | PASS |
| `go build ./...` | PASS |
| `go run ./cmd/architecture-test` | PASS |
| Local binary restart via `./scripts/start-liteaig.sh` | PASS (`admin=200`, `readyz=503` before setup/runtime activation) |

### User Management Closure (2026-09-01)

Multi-user local auth with role-based menu and API convergence:

- `migrations/023_user_management.sql` (owner `identity`) rebuilds `local_admins`
  without the `singleton_key` constraint, adds
  `role CHECK IN ('tenant_admin','tenant_operator','viewer')`, and maps every
  existing row to `tenant_admin`. SQLite has no `DROP COLUMN`, so the table is
  rebuilt in the same migration (temporary `local_admins_new` declared in
  `architecture/table-owners.yaml`).
- `adminapi.Session` carries `Username` + `Role` (`EffectiveRole()` treats an
  empty role as `tenant_admin` for legacy sessions); `LocalVerifier.Verify`
  populates both from the store; `sqlrepo.LocalCredentialStore` gains
  `FindLocalUserByID`/`ListLocalUsers`/`CreateLocalUser`/`SetUserRole`/
  `SetUserStatus`/`DeleteLocalUser`/`CountActiveAdmins`.
- `rbac.PermissionsFor(role)` is the single enumeration of the matrix
  (`tenant_operator` keeps only `approval.decide` + `external_agent.suspend`
  as operator permissions; everything else write is `tenant_admin`).
- New Admin API surface (all `tenant_admin`-gated): `GET/POST
  /api/admin/users`, `PATCH /api/admin/users/{id}/role`,
  `POST /api/admin/users/{id}/status`,
  `POST /api/admin/users/{id}/reset-password`, `DELETE /api/admin/users/{id}`.
  Guards: no self role/status/delete changes (400), last active `tenant_admin`
  protected (409 `LAST_ADMIN_PROTECTED`), min 8-char passwords,
  `USERNAME_TAKEN` on duplicate. `GET /api/admin/me` now returns
  `{username, role, scopes}` and `POST /api/admin/password/change` lets any
  authenticated local user rotate their own password (CSRF-checked).
- Console: `/users` page (admin-only; role/status change, reset password,
  delete, create user), left menu hides Setup/Config/Users for non-admins,
  top-right account dropdown shows username + role tag with change-password
  modal and sign-out; Resources/Alerts/Playground write actions are hidden
  for read-only roles while Governance keeps operator-scoped actions
  (`approval.decide`, `external_agent.suspend`) visible per `me.scopes`.
- Auth state lives in `web/console/src/state/auth.ts` (zustand), hydrated by
  the `me` query inside `queryFn` so menu rendering never races the fetch.
- UI language choice persists to `localStorage` (`liteaig-lang`) so the
  Console stays on the operator's language across reloads.

Verification for this closure:

| Gate | Result |
| --- | --- |
| `go test ./...` (incl. new `adminapi/users_test.go`, rbac, sqlrepo, login) | PASS |
| `go run ./cmd/architecture-test` | PASS |
| `npm run check` (format, locales, hardcoded strings, vitest, build) | PASS |
| Live binary on `18080/18081/18082` — admin login returns `role=tenant_admin` + 7 scopes; create/disable/enable/delete users, role switch, reset-password, self-change all exercised via API | PASS |
| viewer → 403 `ROLE_FORBIDDEN` on `/users` and `/keys`; operator → 403 on `/keys` but approval action reaches handler | PASS |
| Playwright `npm run test:e2e` (setup → login → dashboard → playground incl. SSE streaming → requests → config) | PASS |

## Gate Results

Re-verified in full after the review optimization closure (2026-08-31).

| Gate | Result |
| --- | --- |
| `gofmt -l .` (repo-wide) | clean |
| `go vet ./...` | PASS |
| `go run ./cmd/architecture-test` | PASS |
| `go test -race -count=1 ./...` | PASS (0 failures) |
| `npm run check` (format, locales, hardcoded strings, vitest, build) | PASS |
| `npm test` (tests/contract/sdk) | PASS |
| `actionlint .github/workflows/ci.yml` | PASS |
| `gitleaks --config .gitleaks.toml detect` | no leaks found |
| `openspec validate --all --strict` | 28 passed, 0 failed |
| Postgres + Redis integration (`LITEAIG_TEST_POSTGRES_DSN` :55432, `LITEAIG_TEST_REDIS_ADDR` :56379) — 24 tests, 0 skipped, incl. `-race` | PASS (verified 2026-08-31; re-runs in CI) |
| Real-binary SDK smoke (chat, stream, embeddings, models, Anthropic, MCP, A2A) | PASS |
| Real-binary smoke ×3 consecutive (models, chat, SSE stream, embeddings, MCP, A2A, guardrail block, accounting sources, SIGTERM drain rc=0, restart persistence) | PASS (18/18 each run) |

## Sensitive-Data Notes

- `guardrail_policies.rules` contains non-secret policy patterns only; Security
  Events continue to carry content hashes, never raw prompt/response/secret.
- Tool Call events carry identities and identifiers only; no argument or result
  content is persisted.
- Fast Publish still requires a re-authentication token used only for the one
  request and never persisted.
