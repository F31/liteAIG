# Phase 2 Gap Closure

## Why

The `phase2-commercial-guardrail-cache` change left several "half-done" gaps that reduce production value: Guardrail Policies live only in an in-memory `PolicyRegistry` (lost on restart, no audit), the runtime Guardrail handlers use hard-coded engines instead of snapshot-compiled policies, streaming Layer 3 (async shadow) is not implemented, cache/spool metrics are not wired to a real sink, and Tool Calls / Security Events surfaces return empty tables. Closing these makes Phase 2 genuinely production-ready and unblocks Phase 3 (FinOps/reporting depends on real security-event and tool-call data).

## What Changes

- Persist Guardrail Policies in the database (`guardrail_policies` table, owner guardrail), record publish audit events with prior/new Security Epoch, and load the active policy per Tenant on startup so Fast Publish survives restarts.
- Compile the active Guardrail Policy rules into `TenantRuntimeSnapshot` so the Stage 2/6 guardrail handlers evaluate the snapshot-compiled policy (Tighten/Loosen takes effect at runtime) instead of a fixed engine.
- Add streaming Layer 3 async shadow guard: released content is sent to an External Guardrail in the background (never on TTFT), a violation during an active stream terminates further output, and already-emitted content is recorded as `BLOCK_RETROACTIVE` with post-hoc disposition.
- Wire cache metrics (hits/savings/evictions) and accounting-spool metrics into a concrete `MetricSink` adapter so they reach the existing observability sink.
- Add backend list endpoints for Security Events and Tool Calls and render them in the localized Governance Console instead of empty tables.
- Verify the runtime pipeline composition (all seven stages + cache + tool + guardrail handlers) assembles into a runnable Data Plane with a contract test.

## Capabilities

### New Capabilities
- `guardrail-policy-persistence`: durable tenant-scoped guardrail policies with publish audit and snapshot compilation.
- `guardrail-streaming-shadow`: Layer 3 async shadow guard with retroactive block disposition.

### Modified Capabilities
- `commercial-guardrail`: streaming guardrail now includes Layer 3, and runtime enforcement consumes snapshot-compiled policies.
- `mcp-tool-governance`: Tool Call events are queryable through a tenant-scoped API and surfaced in the Console.

## Impact

- **Backend**: new `guardrail_policies` migration (owner guardrail); `sqlrepo.GuardrailPolicyStore`; `PolicyRegistry` gains a persistence backend and startup load; `compiler.go` compiles active policy rules into the snapshot; `gateway/guardrail` reads the snapshot policy; Layer 3 shadow guard in `guardrail/stream.go`; metric adapters in `observability`; new admin API list endpoints for security events and tool calls.
- **Console**: Governance page renders real Security Events and Tool Call lists (localized, zh-CN/en-US parity).
- **Dependencies**: no new third-party runtime dependencies.

### Non-Goals
- Not wiring a real OTel exporter (Phase 3); not building MCP Tasks extension (Phase 4 A2A); not implementing full HTTP gateway runtime composition beyond a verified pipeline assembly contract.

**Golden Scenarios:** strengthens Scenario D (guardrail lifecycle + streaming Layer 3 + traceable events) and Scenario E (tool call attribution queryable).
