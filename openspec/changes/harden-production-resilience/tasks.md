## 1. Coordination Contracts and Runtime Compilation

- [x] 1.1 Freeze the Redis/Valkey client and Lua runtime versions and add the coordination contracts for ledger, lease, pool, and circuit behind replaceable interfaces.
- [x] 1.2 Extend the config compiler and TenantRuntimeSnapshot with budget windows, lease capacity, credential pools, full circuit parameters, and drift metadata.
- [x] 1.3 Add architecture CI rules for the new `organization`, coordination, and observability package boundaries and table-owner entries.
- [x] 1.4 Add a Redis container conformance fixture for Standard and a self-contained Lite fallback that does not require Redis.

## 2. Distributed Budget Ledger

- [x] 2.1 Implement the atomic multi-window reserve Lua over Tenant/Project/Key windows with hash-tag key layout and bounded TTLs.
- [x] 2.2 Implement idempotent reconcile/release Lua with status guards and ALREADY_FINALIZED semantics.
- [x] 2.3 Implement the Reservation Sweeper with bounded expiry ZSET scans and idempotent expiration.
- [x] 2.4 Implement hard fail-closed and soft fail-open behavior during ledger unavailability with alerts.
- [x] 2.5 Add asynchronous PostgreSQL reservation audit/recovery records that never affect admission correctness.
- [x] 2.6 Add contract tests for atomicity, duplicate reconcile, sweep recovery, and crash-without-release recovery; run against Redis and Lite.

## 3. Concurrency Leases

- [x] 3.1 Implement ZSET lease acquire with expired-member cleanup, capacity check, and bounded key TTL.
- [x] 3.2 Implement renew (update-only) and release (remove-only) with stream renew on ttl/3.
- [x] 3.3 Wire circuit-aware leasing so open circuits skip lease acquisition and half-open probes still lease.
- [x] 3.4 Add race, timeout, and crash-recovery tests proving capacity is never permanently consumed.

## 4. Credential Pools

- [x] 4.1 Implement pool selection strategies (round-robin, weighted, least-inflight, quota-aware) over eligible credentials.
- [x] 4.2 Implement 429/credential-exhaustion rotation within total call budget and per-credential attempt attribution.
- [x] 4.3 Add per-credential circuit, health, and cost accounting and compile pool membership into snapshots.
- [x] 4.4 Add contract tests for eligibility, weighted selection, rotation, and pool exhaustion with no eligible credentials.

## 5. Full Circuit State Machine

- [x] 5.1 Replace the Phase 0 breaker with the complete state machine including configurable sample/threshold/cooldown/escalation.
- [x] 5.2 Implement exclusive half-open probes and deterministic drift-triggered resets with transition events.
- [x] 5.3 Persist or replicate durable circuit facts after decisions so restarts reconstruct state without double-counting probes.
- [x] 5.4 Add deterministic-clock tests for open/probe/close/reopen, cooldown escalation, drift reset, and restart reconstruction.

## 6. Config Drift and Reconciliation

- [x] 6.1 Implement drift detection comparing live runtime to the published version with severity and metrics.
- [x] 6.2 Implement reconciliation that re-compiles and atomically re-activates the published snapshot with audit events.
- [x] 6.3 Couple gateway readiness to snapshot presence, drift grace, and Security Epoch staleness.
- [x] 6.4 Add tests for unexpected divergence, reconciliation restore, and readiness gating during drift.

## 7. Organization and Attribution

- [x] 7.1 Add owner-annotated migrations for users, org_units, user_org_assignments, and cost_centers.
- [x] 7.2 Implement effective-dated membership repositories with tenant-scoped contracts for SQLite and PostgreSQL.
- [x] 7.3 Extend Usage and Request facts with user, OrgUnit, org path, cost center, and attribution trust snapshots.
- [x] 7.4 Add User/OrgUnit budget and RPM dimensions gated on trusted identity attribution.
- [x] 7.5 Add tests for effective-dated reassignment, history preservation, cross-analysis accuracy, and untrusted-label exclusion.

## 8. Isolation Hardening

- [x] 8.1 Generate and enable PostgreSQL RLS policies for Tenant-scoped tables with a restricted application role and a separate platform role.
- [x] 8.2 Add Redis namespace isolation for budget, lease, and coordination keys.
- [x] 8.3 Add the isolation regression suite covering RLS lateral movement, cross-tenant lookup denial, and Redis namespace separation; wire into CI.
- [x] 8.4 Update the architecture manifests and migration ownership for the new isolation constraints.

## 9. Observability and Alerting

- [x] 9.1 Wire the OTel exporter behind the event contract; add request lifecycle spans correlated with Request ID and snapshot version.
- [x] 9.2 Add low-cardinality Prometheus/OTel metrics with a label validation test.
- [x] 9.3 Separate Core, provider-network, and external-guardrail latency measurements.
- [x] 9.4 Implement the Alert lifecycle (firing, acknowledged, silenced, resolved), typed rule builder, and Webhook/Console notification channels.
- [x] 9.5 Implement basic cost-anomaly rules over aggregated usage with evidence-window metadata.
- [x] 9.6 Add tenant-scoped alert state, audit for lifecycle actions, and tests for threshold, ack, and anomaly scenarios.

## 10. Production Operations Console

- [x] 10.1 Add Decision Timeline to Request Explorer with full stage/evidence/attempt rendering.
- [x] 10.2 Implement summary-only Request Live Tail over SSE with Last-Event-ID, reconnect, heartbeat, sampling, and tenant-scope enforcement.
- [x] 10.3 Add Health & Circuits surfaces with confirmed circuit reset operational actions and audit.
- [x] 10.4 Add Alert Inbox and basic rule builder with high-risk re-authentication.
- [x] 10.5 Add RBAC scope navigation (backend-authoritative, frontend display-only).
- [x] 10.6 Add Config Rebase/Conflict flow on concurrent draft publication.
- [x] 10.7 Add PWA static-shell service worker and mobile Dashboard/Alerts/Requests summaries with no sensitive offline caching.
- [x] 10.8 Add zh-CN/en-US locale parity and hardcoded-string checks for all new Console text and pass axe/accessibility gates.

## 11. Phase 1 Release Readiness

- [x] 11.1 Add Golden Scenario B failure-injection gate covering 429, timeout, 5xx, slow TTFT, credential exhaustion, and circuit open with explainable fallback evidence.
- [x] 11.2 Add a 24-hour soak evidence harness proving no sustained Reservation or Lease leak.
- [x] 11.3 Add Redis/PostgreSQL/Provider chaos scenarios and resolve blocking failures.
- [x] 11.4 Add Live Tail payload privacy assertions and tenant-switch cache/SSE leak tests.
- [x] 11.5 Run architecture, unit, race, repository, contract, isolation, security, benchmark, UI, accessibility, and Golden Scenario B suites and resolve all blocking failures.
- [x] 11.6 Validate this OpenSpec change in strict mode and record Phase 1 DoD evidence before any Phase 2 production task becomes eligible.
