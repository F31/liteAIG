## Why

Phase 0 proved a single-instance governed gateway. Enterprises now need to survive real provider failures (429, timeout, 5xx, slow TTFT, credential exhaustion, circuit open) across multiple replicas without leaking budget, losing attribution, or making decisions unreadable. Phase 1 delivers the production hardening behind Golden Scenario B while keeping the single-binary modular monolith.

## What Changes

- Add a distributed multi-window atomic Budget Reservation Ledger, idempotent Reconcile, and Reservation Sweeper backed by Redis/Valkey Lua, with PostgreSQL used only for audit and recovery.
- Add ZSET-based Concurrency Lease acquire/renew/release with bounded cleanup and no full-table scans.
- Add Credential Pools with round-robin, weighted, least-inflight, quota-aware selection, 429 rotation, and per-credential circuit/health/cost tracking.
- Replace the basic Phase 0 circuit with the complete per-provider/deployment/credential state machine, guarded probes, escalating cooldown, and drift-aware reset.
- Add Configuration Drift detection and reconciliation between published snapshots and live expectations.
- Add the User/OrgUnit base model with effective-dated memberships and Usage Event attribution snapshot, enabling User/OrgUnit budget and attribution dimensions (P1).
- Add Standard/Enterprise Tenant isolation hardening: PostgreSQL RLS as a second defense layer and Redis namespace isolation.
- Add OpenTelemetry spans/metrics with low-cardinality labels and an Alert Engine with rule builder, lifecycle, and notification channels (basic).
- Add production Console surfaces: full Decision Timeline, Request Live Tail (summary-only, no Prompt/Response bodies), Health & Circuits, Alert Inbox, RBAC scope navigation, Config Rebase/Conflict, and PWA shell for Dashboard/Alerts/Requests.
- Add Golden Scenario B as an executable release gate with failure injection.

### Non-Goals

- Exact/Semantic cache, streaming guardrail layers, external guardrails, advanced PII/Prompt Injection (Phase 2).
- Cost-aware smart scoring, Routing Simulator, pricing versions, chargeback, multi-currency, Recommendation backend, Data Residency strict (Phase 3).
- Agentic Governance, MCP/A2A, approvals, Agent Graph, or microservices.
- Full SOC 2/ISO certification; only the underlying product evidence controls are delivered here.

## Capabilities

### New Capabilities
- `distributed-budget-ledger`: Multi-window atomic reservation ledger, idempotent reconcile, sweeper, and Redis-unavailable fail modes.
- `concurrency-lease-coordination`: ZSET-based concurrency lease with acquire/renew/release and bounded cleanup.
- `credential-pool-management`: Credential pool selection strategies, 429 rotation, and per-credential circuit/health/cost.
- `full-circuit-state-machine`: Complete circuit lifecycle, guarded probes, cooldown escalation, drift reset, and transition events.
- `config-drift-reconciliation`: Drift detection and reconciliation for published runtime configuration.
- `organization-attribution`: User/OrgUnit model, effective-dated memberships, usage attribution snapshot, and User/OrgUnit budget dimensions.
- `tenant-isolation-hardening`: PostgreSQL RLS and Redis namespace isolation as Standard/Enterprise defense layers.
- `observability-and-alerting`: OpenTelemetry spans/metrics with low-cardinality labels and the basic Alert Engine lifecycle.
- `production-ops-console`: Decision Timeline, Live Tail, Health & Circuits, Alert Inbox, RBAC scope navigation, Config Rebase/Conflict, and PWA shell.

### Modified Capabilities
- `basic-routing-resilience`: Circuit requirement extends from the Phase 0 process-local breaker to the complete distributed state machine; fallback adds credential rotation through pools.
- `phase0-policy-accounting`: Single-window budget is extended to the distributed multi-window ledger for Standard; rate limiting adds User/OrgUnit dimensions when trusted attribution exists.
- `tenant-scoped-access`: Adds PostgreSQL RLS as the second defense layer for Standard/Enterprise repositories.

## Impact

- Adds Redis/Valkey as a first-class Standard/Enterprise coordination dependency behind replaceable contracts; Lite remains self-contained.
- Adds the `organization` domain and User/OrgUnit/assignment tables; existing Usage Events gain attribution snapshot fields.
- Extends RuntimeSnapshot compilation with credential pools, full circuit config, org indexes, and drift metadata.
- Extends Console with SSE Live Tail, alert and health surfaces, and PWA; all new UI text stays localized.
- Golden Scenario mapping: Scenario B, Provider failure and cost optimization. Phase 1 is not complete until its end-to-end DoD passes.
