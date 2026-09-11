# Implementation Delta — v8.5 baseline follow-ups (P0/P1 hardening)

Status / date: 2026-09-03. This file records security and operational behavior
that the current implementation ships *beyond* the frozen V8.5 plan baseline
(`docs/LiteAIG_AI_Gateway_V8.5.md`) after a P0/P1 hardening pass. Where a
section changes an earlier documented behavior, the old behavior is called out.

## 1. Setup wizard is retryable (P0)

A wizard run that fails **after** committing the initial admin/tenant/project
but **before** publishing the first config version used to deadlock the
install: a retry returned `409 ALREADY_INITIALIZED` and the Console had no
published version to draft from.

Now:
- `POST /api/admin/setup` detects that half-initialized state and rolls it back
  in one transaction before re-bootstrapping. Guards are strict: exactly one
  local admin, one tenant, at most the default project, and **zero** published
  config versions. Anything else is never wiped.
- Provider probing treats `401/403` as a **rejected credential**, not health.
- Failure causes surface as specific codes instead of a generic 500:
  - `SETUP_PROVIDER_CONNECTION_FAILED` (unreachable / SSRF policy)
  - `SETUP_PROVIDER_AUTH_FAILED` (401/403 on the credential)
  - `SETUP_PROVIDER_ERROR` (other upstream error, e.g. bad endpoint shape)
  - `SETUP_MODEL_NOT_FOUND` (selected model absent from the catalog)
  - `SETUP_LOCKED` (remote setup without the bootstrap token; pre-existing,
    previously unmapped in the UI)
- The post-publish verification call (`RunFirstCall`) is advisory: if it fails
  the wizard still returns the virtual key plus `firstCallWarning`.
- `GET /api/admin/setup/status` now also returns `ready` (a config version has
  been published). A half-initialized install reports `initialized:true,
  ready:false` and the Console keeps the setup entry visible for retry.

## 2. Session lifecycle (P0)

Sessions are in-memory (12h TTL). The following mutations now **invalidate all
live sessions of the affected account** immediately:

| Mutation | Endpoint | Effect |
|---|---|---|
| Role change | `PATCH /api/admin/users/{id}/role` | target logged out |
| Enable/disable | `POST /api/admin/users/{id}/status` | target logged out |
| Email change (admin) | `PATCH /api/admin/users/{id}/email` | target logged out |
| Email change (self) | `POST /api/admin/users/me/email` | current session logged out; Console returns to `/login?reauth=1` |
| Admin password reset | `POST /api/admin/users/{id}/reset-password` | target logged out |
| Delete user | `DELETE /api/admin/users/{id}` | target logged out |
| Self password change | `POST /api/admin/password/change` | all sessions of the account logged out; Console returns to `/login?changed=1` |
| Emergency reset | `POST /api/admin/password/reset` | sessions of the reset account logged out |
| Email-verified reset confirm | `POST /api/admin/password/reset/confirm` | (pre-existing) resets all sessions |

UI consequence: self password/email changes now require a fresh sign-in.

## 3. High-risk re-authentication is real (P0)

`POST /api/admin/guardrail/publish` (the only reauth-gated route) previously
accepted any non-empty `X-Reauth-Token`. It now verifies the header against the
**session account's current local password**. Responses:
- missing header or wrong password → `401 REAUTH_REQUIRED`
- session with no local credential (IdP-only) → `403 REAUTH_UNAVAILABLE`

The Console's fast-publish form labels the field "current password".

## 4. Audit completeness (P0)

Writes (new): `user.create`, `user.role_change`, `user.email_change`,
`user.email_change_self`, `user.disable`, `user.enable`,
`user.password_reset_admin`, `user.delete`, `api_key.create`, `api_key.reveal`,
`api_key.revoke`, `project.create`, `credential.create`, `credential.rotate`,
`credential.disable`, `credential.delete`, `auth.login`, `auth.login_failed`,
`auth.password_change`, `auth.oidc_login`.

Pre-existing writes: config publish/rollback/reconcile, guardrail fast publish,
alert actions, emergency password reset (`local_password.emergency_reset`),
email reset code send/confirm.

Read contract change (`GET /api/admin/audit`):
- JSON keys are now lowercase (`id`, `action`, `resourceType`, `resourceId`,
  `result`, `actor`, `occurredAt`). `actor` is resolved to the local username
  via a join (raw `actor_id` or `anonymous` otherwise).
- New optional query params `limit` (default 200, max 1000) and `offset`.

## 5. Key / credential hardening (P0)

- `POST /api/admin/keys` and `POST /api/admin/keys/{id}/revoke` persist the
  mutation and then re-activate the data-plane snapshot. A reconcile failure is
  now surfaced as `500 RUNTIME_REFRESH_FAILED` (the change is durable and takes
  effect on the next successful reconcile/publish/restart) instead of being
  silently swallowed.
- Credential rotation is a single atomic upsert (`credential.rotate`), removing
  the crash window where a Rotate-then-Put sequence left the reference
  unreadable.

## 6. OIDC tenant resolution (P1)

OIDC sessions previously inherited the claim mapping's hardcoded `"default"`
tenant placeholder, which does not match the randomly generated bootstrap
tenant, so SSO users saw an empty Console. Sessions are now created against the
real single tenant resolved at sign-in. Verified by
`TestOIDCSessionResolvesRealTenant`.

## 7. Operator endpoints

- `GET /metrics` on the admin origin: dependency-free Prometheus text
  exposition with process/uptime/goroutines plus request-ledger aggregates
  (`liteaig_requests_total`, `liteaig_request_outcome_total`,
  `liteaig_request_tokens_total{kind=input|output}`,
  `liteaig_request_latency_ms_total`, `liteaig_model_requests_total`,
  `liteaig_model_latency_ms_total`). No prompts, keys, or per-request detail.
- Gateway rate limit is configurable: `--gateway-rpm` (default 600) and
  `--gateway-burst` (default: equal to rpm), per tenant+project.
- `GET /api/admin/health` now performs **real** upstream probes (model catalog
  with the deployed credential, egress policy applies, ~20s cache) and reports
  a `reason` for unhealthy providers: `no_credential`,
  `credential_unavailable`, `invalid_endpoint`, `unreachable`,
  `credential_rejected`, `upstream_unavailable`, `upstream_error`,
  `provider_disabled`, `no_enabled_credential`. The Health page renders the
  provider table.
- Emergency local password reset is usable from the Console login screen: the
  "forgot password" dialog (shown when no recovery email is configured) accepts
  the optional one-time bootstrap token from the server startup log and sends it
  as `X-Bootstrap-Token`. On the server host no token is required.

## 8. Deployment

- Helm: optional persistent SQLite for a single-replica `mode: all` workload.
  Starting from the default `gateway` workload, set `mode=all`, `replicas=1`,
  `database.enabled=true`, `autoscaling.enabled=false`,
  `pdb.minAvailable=0`, and `rollingUpdate.maxUnavailable=0`. The chart then
  creates a PVC (`ReadWriteOnce`, `database.size`/`storageClass`/
  `existingClaim`) and passes `--db=file:/data/liteaig.db`. Template validation
  rejects SQLite on split, replicated, or autoscaled workloads.
- Master key: `LITEAIG_MASTER_KEY` (base64, 32 bytes) overrides the sidecar
  file entirely, documented on `secrets.LoadMasterKey`.
- Repo hygiene: root build logs and pid files removed; `.gitignore` now covers
  `/bin/`, `/logs/`, `*.log`, `web/console/dist/`, `web/console/e2e/.bin/`.

## 9. Console error surface

401 handling on `/api/admin/password/change` and `/api/admin/password/reset`
is now treated as an expected outcome (wrong password / loopback gate) instead
of a stale-session signal, so a single password typo no longer logs the
operator out. Users/Config error toasts surface backend error codes via
`errors.*` i18n keys. Config page has a Rollback action per historical version.

## 10. Deep code-quality review pass (2026-09-04)

Reviewed against the frozen V8.5 principles (modular monolith, three-layer
service architecture, narrow capability interfaces, explicit error contract,
immutable RuntimeSnapshot, executable architecture guards). Findings that were
**implemented** this pass:

- **Simulate no longer swallows routing errors** (`backend/backend.go`): the
  simulator returned a successful-but-empty result when `Planner.Plan` failed.
  It now propagates the error; regression test `TestControlBackendSimulatorSurfacesPlanErrors`.
- **Single error-code → HTTP-status source of truth**
  (`kernel/errors.StatusForCode`): the gateway and admin renderers previously
  kept two near-identical switches that could drift (the gateway one knew
  `FEDERATION_*`/`upstream_*`, the admin one did not). Both now delegate to the
  shared mapping; `TestStatusForCode` locks the table.
- **sqlrepo no longer drops corrupt JSON silently** (`sqlrepo/jsonutil.go`):
  six read paths (alerts, approvals, resource catalog) discarded `json.Unmarshal`
  errors; they now log a corruption warning while keeping best-effort empty values.
- **Dead re-export removed** (`adminapi/backend.go` deleted): it only forwarded
  to `backend.NewControlBackend` and its type aliases had zero references inside
  the package.
- **Strategy/kind vocabularies single-sourced** (`config/validator.go`,
  `guardrail/builtin/engine.go`): route/pool/budget strategy sets and guardrail
  rule kinds were inline string lists repeated per validation site. They are now
  package-level slices (`routeStrategies`, `poolStrategies`, `budgetModes`,
  `budgetConsistency`, `builtin.RuleKinds`) that both validators and the engine
  reference; adding a strategy/kind touches one file instead of N switches.

**Structural batch — stage 1 (implemented 2026-09-04)**:

- **The `Backend` god interface is gone** (`backend/capabilities.go`,
  `adminapi/capabilities.go`): `adminapi` now depends on the 12 narrow
  capability interfaces via a `Services` struct plus three *optional* narrow
  fields (`CredentialOperator`, `PlaygroundStreamer`, `GuardrailPublisher`).
  `adminapi.New(svc Services, authorizer Authorizer)` wires each field
  independently; `adminapi.AllOf[T fullCapabilities](s T)` is a compile-time
  convenience for single-object compositions (no runtime assertions).
- **No more `s.backend.(Xxx)` assertion backdoors** (`adminapi/routes_*.go`):
  the six assertion sites (4× credential operator, playground streamer,
  guardrail publisher) now read the pre-wired optional field and answer
  501 NOT_IMPLEMENTED when it is nil — same behavior, compile-time wiring.
- **Unconditional `errNotWired` stubs deleted from `ControlBackend`**
  (`backend/backend.go`): `Setup`, `CreateProject`, `CreateKey`, `RevokeKey`,
  `ListKeys`, `RevealKey`, `DiscoverMCPTools`, `Playground`, `Audit` no longer
  exist as fakes; the `var _ Backend = (*ControlBackend)(nil)` compile-time
  claim was removed with them. `ControlBackend` now exposes only methods that
  have a real (dependency-conditional) implementation; the composition root
  (`app/lite.go` `liteBackend`) supplies the rest and is verified to satisfy
  every capability by `AllOf` at compile time.

**Structural batch — stage 2 (implemented 2026-09-04)**:

- **Executor attempt loop unified** (`gateway/execution/executor.go`):
  `Execute` and `ExecuteStream` — structurally identical except the call step —
  now delegate to a single `runPlan` core that owns the circuit gate, capacity
  lease, per-attempt timeout, backoff and LEASE_BUSY/exhausted classification;
  each path supplies an `attemptCall` (bounded invoke / stream attempt).
  `ExecuteWithPool` keeps its own loop because credential rotation is a
  genuinely different iteration (per-credential circuit/lease `continue`,
  stateful picker). Stream attempts now carry `CredentialID` for attribution
  (no consumer depended on it being empty); regression
  `TestStreamAttemptsCarryCredentialAndOutcome`.
- **Usage recording unified** (`app/pipeline.go`): the three `finalize*`
  methods rebuilt `accounting.Facts` independently (~150 duplicated lines).
  They now fill a `usageFacts` delta record and share `recordUsage` (single
  place that stamps the event, settles reservations, runs the finalizer and
  publishes the live event) plus `routeEvidenceFromPlan` / `attemptsFromResult`
  mappers. Behavior preserved; failure events additionally record their real
  latency (the live event already reported it; the persisted facts did not).
- **`NewLite` decomposed** (`app/lite.go`): the ~430-line composition root is
  now ~120 lines of orchestration over four builders: `openLiteFoundation`
  (storage/migrations/cipher/pepper/hasher; closes the DB on its own failure),
  `bootstrapLiteRuntime` (global runtime, API-key service, config service with
  guardrail loader, LKG publisher/boot, startup reconcile, alert service),
  `buildLiteAdmin` (sessions, admin server, OIDC/local/password-reset login,
  audit, handler stack) and `wireAccountingSpool` (durable WAL spool for
  file-backed DSNs).

**Structural batch — stage 3, frontend (implemented 2026-09-04)**:

- **Resources god component thinned** (`web/console/src/pages/`, 3151 → 2674
  lines): types, form constants and pure helpers moved to `resources/types.ts`,
  `resources/constants.ts`, `resources/helpers.ts`; the tenant-config draft
  state machine (drafts/versions/restored-draft queries, serialized auto-save
  chain with revision-conflict resync, publish + validation diagnostics,
  abandoned-draft restore) moved to the shared `useTenantConfigDraft` hook in
  `resources/useTenantConfigDraft.ts` — the single "shared draft hook" every
  resource editor consumes, plus `draftRowStates` for the new/modified/deleted
  row badges.
- **E2E net for the Resources page**: the management-loop spec now visits
  `/resources` (title, Model access table, the wizard's deterministic
  `first-key` row) so future refactors of this page have a browser-level
  regression guard.

**Structural batch — stage 4, per-resource editor components (implemented
2026-09-04)**:

- **Resources page is now a thin orchestrator** (`web/console/src/pages/`,
  2674 → 329 lines): every resource table plus its editor drawer(s) moved
  into dedicated section components under `resources/` — `ProviderSection`
  (providers table with expandable credential sub-table, provider editor
  drawer, credential create drawer, credential rotate drawer, and the four
  credential mutations), `DeploymentSection` (incl. the provider-scoped
  credential options + provider-switch credential-sync effect),
  `RouteSection` (deployment multi-select with missing-resource
  placeholders), `LogicalModelSection` (route policy select with
  missing-resource fallback), `MCPServerSection` (servers + tools tables,
  discovered-tools panel, tool discovery + apply), `KeySection` (keys query,
  create/reveal/revoke mutations, one-time reveal panel), `ProjectSection`
  (creation form + table) and `AuditSection` (audit query + table). Each
  section owns its form instance, drawer open/editing-ID state and
  open/save/disable/delete handlers; the page keeps the shared queries, the
  `useTenantConfigDraft` state machine, draft row badges, and the publish
  bar.
- **Shared derivations extracted**: `useResourceRows` (`resources/
  useResourceRows.tsx`) computes every table row set (draft-union of draft +
  runtime per collection) and the cross-resource select options (deployment /
  route options with draft-state tags and searchable labels) once for all
  sections; `ResourceStatusTag` is the shared per-row status/draft badge;
  translated strategy labels/descriptions moved to `constants.ts`;
  `useTenantConfigDraft` gained `updateConfig` (the draft-mutation helper the
  save handlers share). Section prop contracts live in `resources/
  sections.ts`.
- **Dead code removed**: the generic fallback tab loop (`groups` filter
  excluded every member, always producing zero tabs) with its `table`
  helper and generic `columns` was deleted.
- **Behavior notes**: one latent copy-paste fixed in passing — the
  credential sub-table status column was keyed to the `deployments` draft
  map; it is now keyed to `credentials` (renders identically: credentials
  are runtime-only, never draft-badged). Drawer open state and form values
  now live in the section component, so abandoning a tab mid-edit resets
  that drawer on return (the drawer itself always unmounted with the tab
  pane); the draft itself is unaffected (page-level hook).
- **Verification**: `npm run check` (prettier / i18n / strings / vitest 9 /
  tsc app+node / vite build), `go build` + `go vet` + `make check` (arch
  test + console), dev instance rebuilt with the embedded console
  (readyz 200 / admin 401 / gateway 400 smoke), and the management-loop e2e
  (incl. the `/resources` steps) against a freshly built e2e binary — 1
  passed.

**Structural batch — stage 5, snapshot getter boilerplate / M7 (implemented
2026-09-04)**:

- **Snapshot copy logic single-sourced** (`kernel/runtime/snapshot.go`, 783
  → 715 lines): the ~30 hand-rolled getters each re-implemented the
  deep-copy-on-return logic, and the constructor duplicated it a third
  time. Each record type with mutable fields now has exactly one clone
  function (`cloneProject`, `cloneDeployment`, `cloneRoutePolicy`,
  `cloneAPIKey`, `cloneCredentialPool`, `cloneTool`, `cloneToolPolicy`,
  `cloneAgent`, `cloneAgentEndpoint`, `cloneCachePolicy`,
  `cloneGuardrailPolicy` over `cloneStrings` / `cloneBytes`), used by both
  `NewTenantSnapshot` and the getters. Collection getters share
  `snapshotCollect` / `snapshotCollectSorted` (the two deterministic
  orderings — `LogicalModels` by alias, `BudgetPolicies` by id — are
  preserved); value-only types use `identity[T]`. All getters are now
  uniform one-liners; behavior is unchanged (same copy semantics, same
  orderings).
- **Copy-isolation regression net added** (`snapshot_test.go`): the
  deep-copy-on-return contract was previously untested. New tests mutate
  every returned single-item value (Project / Deployment / RoutePolicy /
  APIKey incl. the `ExpiresAt` pointer / CredentialPool / Tool /
  ToolPolicy / Agent / AgentEndpoint / GuardrailPolicy) and every
  collection getter's entries and assert the stored snapshot is untouched,
  plus `NewTenantSnapshot` source-data isolation and the deterministic
  collection orderings.
- **Verification**: `go build` / `go vet` / full `go test -race ./...` /
  `make check` (architecture boundaries ok), dev instance rebuilt + smoke
  (readyz 200 / admin 401 / gateway 400), management-loop e2e against a
  freshly built e2e binary — 1 passed.

**Structural batch — stage 6, `liteBackend` override-surface shrink
(implemented 2026-09-04)**:

- **Capability slots moved into `ControlBackend`** (`controlplane/backend`):
  the `liteBackend` type (which embedded `*ControlBackend` and overrode 17
  methods to add the runnable Lite paths) is gone. Instead `ControlBackend`
  carries 17 optional function-valued "capability slots" — wizard setup, the
  API-key lifecycle, provider credentials, MCP discovery, playground
  (sync + stream), projects, audit, health, and the federation overlay —
  each set through a `Set*` method and each answering `errNotWired` (HTTP
  501) when left unwired. `Projects` / `Health` / `Federation` keep their
  snapshot-derived defaults and consult the slot first when present. This
  matches the existing optional-slot pattern (`SetApprovalLister`,
  `SetGuardrailRegistry`, …), so a control-plane-only profile stays
  constructible without those paths.
- **`app/lite.go` composition simplified**: the Lite paths now live on a
  plain `liteCapabilities` struct (no embedding, 16 fields down to 13 — the
  now-dead `federation` and `secretProvider` fields were dropped) and are
  wired into the backend with explicit `Set*` calls in `NewLite`.
  `adminapi.AllOf(controlBackend)` now compiles against the bare
  `*ControlBackend` — the generic constraint is the compile-time check that
  every capability interface is satisfied. The audit-event closure is created
  once in `NewLite` and passed to `buildLiteAdmin` (which no longer
  re-declares it).
- **Verification**: `go build` / `go vet` / full `go test -race ./...` /
  `make check` (architecture boundaries ok), dev instance rebuilt + smoke
  (readyz 200 / admin 401 / gateway 400), management-loop e2e against a
  freshly built e2e binary — 1 passed.

With stage 6 closed, the V8.5 deep-review action list (P0 six, P1 A–E, review
batch 1, structural stages 1–6) is fully implemented; there are no remaining
structural items from the review.
