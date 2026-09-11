# Phase 2 Gap Closure — Design

## Context

`phase2-commercial-guardrail-cache` shipped the policy model, detectors, streaming Layer 1/2, External Guardrail contract, and Security Events, but the active `PolicyRegistry` is in-memory only (`NewPolicyRegistry(nil)` with a nil audit), runtime guardrail handlers are constructed with a fixed `builtin.Engine`, and the Security Events / Tool Calls Console surfaces are empty. This change makes the guardrail policy durable and runtime-effective and completes the streaming and observability story.

## Goals / Non-Goals

**Goals:**
- Guardrail policies persist across restarts, Fast Publish records an audit event with prior/new Security Epoch, and the active policy compiles into `TenantRuntimeSnapshot` so Stage 2/6 enforce it at runtime.
- Layer 3 async shadow guard with `BLOCK_RETROACTIVE` disposition.
- Cache + accounting-spool metrics flow through a concrete `MetricSink` adapter.
- Security Events and Tool Calls are queryable and rendered in the Console.

**Non-Goals:**
- Real OTel exporter wiring, MCP Tasks extension, full HTTP gateway runtime (beyond a pipeline assembly contract test).

## Decisions

### 1. Guardrail policy is a tenant-scoped persisted resource
Add a `guardrail_policies` table (owner guardrail) with columns for id, tenant_id, version, security_epoch, change_type, rules (JSON), actor, and published_at. `PolicyRegistry` gains an optional `Store` backend: `FastPublish` writes the row and appends an `audit_events` row (observability-owned via the existing config-audit pattern); `NewPolicyRegistry` loads the latest row per tenant. This mirrors how `config.Service.Publish` persists and audits.
Alternative considered: keep in-memory only. Rejected — policies lost on restart violates the lifecycle spec and blocks runtime compilation.

### 2. Snapshot-compiled guardrail policy
The config document gains a `GuardrailPolicy` (rules JSON + mode), the compiler emits `runtime.GuardrailPolicy` into `TenantSnapshotData`, and `TenantRuntimeSnapshot` exposes `GuardrailPolicy()`. The Stage 2/6 `gateway/guardrail.Handler` builds its `builtin.Engine` from the snapshot policy at Handle time (or holds a compiled engine refreshed on activation), so Tighten/Loosen is effective immediately after publish without a full config republish.
Alternative considered: handler reads `PolicyRegistry` directly. Rejected — the Data Plane must read only immutable snapshot views (V8.2 §2.7.3).

### 3. Layer 3 is async shadow, off the TTFT path
`ShadowGuard` receives already-released content, sends it to an `ExternalProvider` in a background goroutine, and holds a per-request stop channel. If a violation returns while the stream is still active, it signals the stream to stop further output; already-emitted content is recorded as `BLOCK_RETROACTIVE` (Security Event + event kind) since it cannot be retracted.
Alternative considered: synchronous Layer 3. Rejected by V8.2 §16 (Layer 3 must not enter TTFT).

### 4. Metrics reach the existing sink via an adapter
`observability` exposes `MetricSinkAdapter` that implements `cache.Metrics` and `accounting.SpoolMetrics` by forwarding to `observability.MetricSink.Record`, keeping domains dependency-free (the adapter lives in `internal/app` composition or `observability`).
Alternative considered: domains import observability. Rejected by the event-contract rule.

### 5. Tool Calls are queryable
Add a `tool_call_events` table (owner observability, Workstream B) populated by the MCP connector's sink; a tenant-scoped `GET /api/admin/tool-calls` endpoint and `GET /api/admin/security-events` endpoint serve the Governance page.

## Risks / Trade-offs

- [Fast Publish + snapshot activation race] → publish writes DB then recompiles/activates the tenant snapshot atomically; in-flight requests keep their captured version.
- [Layer 3 stops mid-stream] → bounded by stream lifetime; already-sent chunks are retroactively flagged, never retracted.
- [Snapshot carries rule patterns] → rules are non-secret config; PII/secret patterns are policy, not credentials.

## Migration Plan

1. Land `guardrail_policies` + `tool_call_events` migrations; add `sqlrepo.GuardrailPolicyStore` and `ToolCallStore`.
2. Extend `PolicyRegistry` with a store + startup load + audit; compile policy into snapshot.
3. Update `gateway/guardrail` to use snapshot policy; add Layer 3 `ShadowGuard`; wire `tool.call` events into the store.
4. Add metric adapters and the two list endpoints; render real lists in Governance.
5. Add a pipeline assembly contract test; run the full gate set; validate OpenSpec strict.

Rollback: each addition is behind config defaults; a nil store keeps the in-memory registry, and a nil snapshot policy keeps handler behavior as-is.
