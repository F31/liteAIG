## Context

Phase 0 shipped a single-binary governed gateway with process-local budget, lease-free execution, basic circuit, SQLite/PostgreSQL persistence, and an embedded localized Console. Phase 1 must make that baseline production-grade across replicas without breaking the modular monolith: correctness now comes from Redis/Valkey atomic coordination plus PostgreSQL RLS as a second defense layer, while Lite remains self-contained.

Frozen V7.2 constraints continue to apply: fixed seven-stage pipeline, immutable compiled snapshots, canonical Principal ownership, One Table One Owner, server-side Draft/Validate/Diff/Publish, and React/TypeScript localization. Golden Scenario B (provider failure and cost optimization) is the acceptance outcome.

## Goals / Non-Goals

**Goals:**

- Correct multi-replica Budget and concurrency accounting with bounded recovery after crashes.
- Explainable, attributable production behavior: full circuit, credential pools, fallback/rotation evidence, and User/OrgUnit attribution.
- Defense-in-depth isolation (RLS + Redis namespaces) with a regression suite.
- OTel spans/metrics and an Alert lifecycle that never expose Prompt/Response bodies.
- Operations surfaces (Decision Timeline, Live Tail, Health & Circuits, Alert Inbox, PWA) built on the existing localized Console.
- Golden Scenario B as an executable gate.

**Non-Goals:**

- Cache engines, streaming guardrail layers, external guardrails, advanced PII/Prompt Injection.
- Smart scoring, Routing Simulator, pricing versions, chargeback, multi-currency, Recommendations, strict Data Residency.
- Agentic Governance, MCP/A2A, approvals, Agent Graph.
- Microservices or internal network boundaries.

## Decisions

### 1. Keep the ledger authoritative in Redis/Valkey, PostgreSQL for recovery only
Multi-window Budget correctness uses a single Lua script per operation (reserve, reconcile, release, sweep) so admission-time decisions never depend on asynchronous PostgreSQL writes. PostgreSQL stores reservation history purely for audit and recovery. Lite keeps SQLite/local counters.
Alternative considered: PostgreSQL transactions as the authority. Rejected because cross-replica admission would either serialize on the DB or become inconsistent with the frozen multi-replica Standard target.

### 2. Concurrency via ZSET leases
Acquire/Renew/Release use ZSET Lua with expiry scores and bounded PEXPIRE; capacity checks remove expired members first. No HGETALL full scans. Streaming renews on `ttl/3`. Cleanup is opportunistic plus a background job.
Alternative considered: Hash-based leases with full scans. Rejected by the frozen V7.2 decision (#27) and O(log N) requirement.

### 3. Credential pools compiled into the snapshot
Pool membership, strategy (round-robin/weighted/least-inflight/quota-aware), and per-credential state are compiled into the immutable Tenant Runtime Snapshot. Execution selects via a pool contract and rotates on retryable 429/credential exhaustion; per-credential circuit/health/cost stay attributed.
Alternative considered: resolve pools per request from the DB. Rejected to preserve snapshot-only hot-path reads.

### 4. Complete circuit as a contract with durable facts
The full state machine (Closed→Open→Half-Open with escalating cooldown, exclusive probes) replaces the Phase 0 breaker behind the existing `Circuit` contract. Hot-path probes stay process-local; durable transition facts are written after the decision so a restart reconstructs state without double-counting probes.
Alternative considered: fully distributed circuit in Redis per decision. Rejected because it adds latency on every request for a rare transition; durable-facts-after-decision keeps the hot path fast and still survives restart.

### 5. Drift as a reconciliation command, not a config bypass
Drift detection compares live runtime against the published version; reconciliation re-compiles and atomically re-activates the published snapshot, emitting an audit event. Ordinary toggles cannot use drift as a backdoor around Draft/Publish.
Alternative considered: allow drift path to mutate config directly. Rejected as a validation bypass.

### 6. OrgUnit/User attribution with effective-dated assignments
Users and OrgUnits are canonical identity dimensions; User↔OrgUnit assignments carry validity intervals; Usage Events snapshot org_unit_id/org_path/cost_center/attribution_trust at call time. Budgets may add User/OrgUnit windows only when trusted identity exists.
Alternative considered: derive org from current assignment only. Rejected because personnel moves must not rewrite history.

### 7. RLS as a real second layer, not documentation
Standard/Enterprise repository connections use RLS-restricted roles; platform ops use a separate controlled role. Migrations enable RLS policies per Tenant-scoped table; the isolation suite runs in CI.
Alternative considered: repository scope only. Rejected by frozen decision #13 and the SaaS multi-tenant requirement.

### 8. OTel through the event contract
Business modules keep emitting standard events; the Platform layer wires OTel exporter, spans, and low-cardinality metrics. External/provider latency is recorded separately from Core overhead.
Alternative considered: modules call OTel directly. Rejected by the frozen Observability contract (#93).

### 9. Live Tail is summary-only SSE
Live Tail streams request summaries (id, scope, status, latency, deployment) with Last-Event-ID and reconnect; Prompt/Response bodies never enter the stream, enforced by construction of the payload type and server-side RBAC.
Alternative considered: stream full records with redaction. Rejected because it risks accidental body inclusion and contradicts the frozen Live Tail privacy decision (#42).

### 10. PWA caches only the static shell
Service worker caches static assets only; Admin API, Usage, Prompt, Response, Secret, and Usage detail are `no-store`. High-risk actions require re-auth; frontend RBAC is display-only.
Alternative considered: broader offline caching. Rejected by the frozen PWA security decisions (#47-48).

## Risks / Trade-offs

- [Redis outage blocks hard budget] → Declared per Project fail-closed/soft fail-open with alerts; Lite unaffected.
- [Sweeper races with in-flight reconcile] → Idempotent Lua and status-guarded transitions make double-finalization a no-op.
- [Restart reconstruction of circuits may briefly allow extra probes] → Accept bounded in-flight window; durable transition facts bound the retry behavior.
- [RLS policy maintenance burden] → One manifest-driven policy generation plus the CI isolation suite.
- [Live Tail scale] → Sampling with explicit rate and heartbeat; payload types make body leakage structurally impossible.
- [OrgUnit budget without trusted identity] → Enforcement is gated on attribution trust; untrusted requests never use those dimensions.
- [PWA surface growth] → Keep mobile scope to summaries and operational quick actions; complex editors stay desktop.

## Frozen Decisions (recorded during implementation)

- Standard/Enterprise coordination uses `github.com/redis/go-redis/v9` v9.22.0 against Redis/Valkey; Lua scripts run server-side in the embedded Redis Lua runtime (no external Lua dependency).
- Coordination primitives (Budget Ledger, Concurrency Lease, namespace key helpers) live under `internal/platform/coordination` behind replaceable contracts; domain code depends only on the contracts.
- Lite uses in-memory contract implementations behind the same interfaces so it never requires Redis.
- PostgreSQL RLS is a Platform-owned operation (`internal/platform/storage/postgres/rls.go`), not a portable schema migration. It creates the restricted `liteaig_app` role, the controlled `liteaig_platform` role (BYPASSRLS for operations only), grants public-schema USAGE, and applies per-table `tenant_isolation` policies keyed on `app.tenant_id`. This avoids SQLite incompatibility and respects One Table One Owner (no single migration touches multiple owners' tables).
- All Redis/Valkey coordination keys embed the tenant hash tag (`{tenant:<id>}`) so budget, lease, and coordination state is namespace-isolated across tenants by construction.

## Migration Plan

1. Add Redis/Valkey contracts behind interfaces; Lite continues with local implementations.
2. Add Budget ledger, lease, pool, and circuit modules behind existing Kernel contracts; compile their config into snapshots.
3. Add RLS policies and Redis namespacing; extend the isolation suite.
4. Add OrgUnit/User tables, attribution snapshot fields, and a trusted-identity verifier integration point.
5. Wire OTel and the Alert Engine over the event contract.
6. Add Decision Timeline, Live Tail, Health & Circuits, Alert Inbox, Rebase/Conflict, and PWA surfaces to the Console with zh-CN/en-US parity.
7. Add Scenario B chaos/failure-injection gate and the 24h soak evidence harness.

Rollback: each capability stays behind a config flag or contract; Redis-related features disable to Lite semantics, and migrations are forward-compatible.

## Open Questions

- Which Redis/Valkey client and Lua runtime should be pinned for Standard (given existing Go dependency policy)?
- Should the 24h soak run in CI nightly or as a separate release harness?
- Which trusted identity source (OIDC/JWT) gates User/OrgUnit budget enforcement in this phase versus Phase 4?
- Are RLS policies enabled on all Tenant-scoped tables now, or iteratively per table as Phase 1 lands?
