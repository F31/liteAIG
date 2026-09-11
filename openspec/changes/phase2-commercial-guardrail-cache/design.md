# Phase 2 — P0-Commercial Guardrail + Cache: Design

## Context

Phase 0 left the Guardrail Engine at deterministic Fast Guard (`internal/guardrail/builtin/engine.go`: keyword/regex/secret rules with block/redact), already executed at Stage 2 (input) and Stage 6 (output) via `internal/gateway/guardrail/handler.go`. The pipeline exposes `Directive.SkipResolutionExecution` (`internal/kernel/pipeline/runner.go:16`) as a cache-compatible short circuit that no implementation uses. The console has no Guardrail, Cache, or MCP/Tool surfaces. There is no Agent Identity beyond `Principal.AgentID` binding, no Task ID propagation beyond `interaction.Context` fields, and no MCP adapter.

Phase 2 closes the gap to "P0-Commercial": a governed Guardrail lifecycle (Test → Impact Preview → Publish → Security Event), an Exact Cache that never bypasses Output Guardrail, and the Agentic foundation (Agent Identity + MCP Tool governance + Task ID plumbing) that the V8.2 Federated Agent Trust program depends on.

## Goals / Non-Goals

**Goals:**
- Exact Cache with tenant/project-scoped keys and cache-savings metrics; cache hits short-circuit Stage 4/5 but always run current Output Guardrail (Stage 6) and Accounting (Stage 7).
- Commercial Guardrail: basic PII and Prompt Injection detection, explicit input/output execution, streaming Layer 1 (inline fast) and Layer 2 (buffered local), replaceable External Guardrail API with explicit fail modes, Security Events traceable to policy/rule/request, and Guardrail Policy Fast Publish (Tighten/Loosen).
- MCP 2026-07-28 Tool governance: Streamable HTTP adapter, `server/discover`, Tool Registry/Catalog, Tool ACL, schema/DLP on arguments, Content Provenance marking tool results untrusted, Tool Call Events.
- Agent Identity as a canonical Principal type and Task ID plumbing across Request Explorer, Accounting, and Telemetry.
- Localized Console: Guardrail Policy Editor, Test Cases, Guardrail-only Playground, Impact Preview (explicit samples only), Security Events, Cache Overview/Policies/Savings, MCP Servers/Tool Catalog/Tool Policy/Tool Calls.

**Non-Goals:**
- Semantic Cache (Phase 4), A2A adapter, Delegation/Approval, Federated Agent Trust (Phase 4).
- LLM-as-Judge, advanced DLP, Groundedness (Phase 4); Capability Routing (Phase 5).
- Any change to the fixed seven-stage pipeline order.

## Decisions

### 1. Cache is a replaceable contract with Memory-L1 + Valkey-L2 tiers
Define a `Cache` contract in `internal/cache` with key `hash(tenant_id, project_id, logical_model, normalized_request, semantic_affecting_params, cache_namespace_version)` — never shared across Projects by default. Implement a Memory LRU (Lite) and a Redis/Valkey-backed store (Standard/Enterprise) reusing the coordination client; namespace keys by tenant/project. `cache_namespace_version` compiled from the snapshot enables deterministic invalidation.
Alternative considered: PostgreSQL-backed cache. Rejected — cache must not sit on the DB hot path; Redis/Valkey is already a Standard dependency.

### 2. Cache hits route through the existing directive but keep governance
Wire the cache into Stage 3 (Policy & Cost Preflight). On a hit, return `SkipResolutionExecution`, store the normalized upstream response, then re-run current Output Guardrail (Stage 6) and Accounting (Stage 7) on the cached response. Default cacheable: deterministic/low-temperature, embeddings, classification/extraction/translation, explicit `cache=true`. Default not cached: tool calls, high temperature, Guardrail blocks, high-sensitivity PII/secret policy, `Cache-Control: no-cache`.
Alternative considered: storing post-guardrail responses. Rejected — violates the frozen invariant that cached output must pass the *current* Output Guardrail version.

### 3. Guardrail lifecycle is a publishable typed Policy, not a filter list
Model a Guardrail Policy (versioned rules, Tighten/Loosen semantics) compiled into the snapshot, with Fast Publish updating guardrail policy and Security Epoch without full config publication. Add basic PII and Prompt-Injection detectors alongside the existing Fast Guard within the same `internal/guardrail` domain, keeping the three-layer streaming model.

### 4. Streaming guardrail is three layers; P2 ships Layer 1 + Layer 2
- Layer 1 inline fast guard: per chunk before write, local keyword/regex/secret/basic PII, cross-chunk rolling window, ≤0.3 ms/chunk budget, MASK or BLOCK.
- Layer 2 buffered local: release windows at sentence boundary / newline / 64 tokens, ≤20 ms window budget.
- External Guardrail API (replaceable contract, own fail modes: fail-open/fail-closed/bypass, timeout observable) enables Layer 3 async shadow but is never a synchronous per-chunk call by default.
Alternative considered: synchronous per-chunk remote guardrail. Rejected by V8.2 §16 (Layer 3 must not enter TTFT).

### 5. MCP 2026-07-28 as a Tool adapter, not a model protocol
Add `internal/access/protocol/mcp` (stateless Streamable HTTP, `Mcp-Method/Mcp-Name`, `server/discover`) and `internal/connectors/tool` (MCP Tool connector) behind the existing `Invoker` contract. Tool Registry/Catalog and Tool ACL compile into a new snapshot `ToolIndex`/`ToolPolicy`; `TOOL_REQUEST` enforcement runs in the pipeline (Input Guardrail stage) so ACL is not bypassable by Prompt Injection. Tool results carry `Content Provenance=tool_result` (untrusted) and re-enter guardrail; Tool Call Events feed Workstream B.

### 6. Agent Identity stays on the canonical Principal; Task ID is plumbed, not re-modeled
`Principal.Type=agent` (already has `AgentID`) is the only Agent identity; a minimal `AgentIndex` snapshot entry records agent identity/status without a second identity subsystem. Task IDs (`task_id/root_task_id/parent_task_id`) propagate from `interaction.Context` into `accounting.Facts`, `RequestRecord`, `UsageRecord`, and OTel span attributes — no new trace fact source.
Alternative considered: a dedicated `agentic/identity` model. Rejected by V8.2 §2.6.4 (one canonical Principal) and Workstream B (reuse OTel + events).

### 7. All thresholds, cacheability rules, guardrail detectors, and fail modes are validated typed configuration
Cache policy, cacheability rules, PII/Prompt-Injection patterns, streaming window budgets, External Guardrail fail modes, and MCP/Tool settings are compiled into the Tenant Runtime Snapshot with explicit defaults and validation — no scattered constants. Console additions use zh-CN/en-US JSON parity and no hard-coded strings; high-risk guardrail/tool actions require backend re-auth.

## Risks / Trade-offs

- [Stale cache serves outdated output] → cache namespace version in the key + policy-driven invalidation; tenant isolation test.
- [Cache hit bypasses guardrail] → cached response re-runs current Output Guardrail; dedicated cache-hit-still-guards test (Scenario D).
- [Streaming Layer 1/2 add TTFT latency] → tight per-chunk/window budgets; Layer1/2 TTFT regression ≤20% gate.
- [External Guardrail slows or fails] → explicit fail modes, timeout observability, never per-chunk synchronous by default.
- [Tool ACL bypassed by Prompt Injection] → enforcement in pipeline checkpoint + non-bypass test (Scenario E).
- [Tool results treated as trusted] → untrusted provenance re-guardrail + provenance field in Security Events.
- [PII/secret in Security Events] → events carry content hash/redaction only; secret-leak regression tests.

## Migration Plan

1. Land cache contract + Memory/Valkey stores and wire the Stage 3 short circuit (defaults keep cache disabled until a Policy enables it).
2. Extend Guardrail domain with PII/Prompt-Injection detectors and the publishable Policy lifecycle; add Security Events and Fast Publish.
3. Add streaming Layer 1/2 and the External Guardrail API contract.
4. Add MCP adapter + Tool Registry/ACL + Content Provenance + Tool Call Events; extend snapshot with Tool/Agent indexes.
5. Add Task ID plumbing across Accounting/Request Explorer/OTel; add localized Console surfaces.
6. Add tests per slice (cache-hit-still-guards, stream cross-chunk, TTFT regression, Tool ACL non-bypass, secret-leak, tenant isolation) and run the full gate set; validate OpenSpec in strict mode.
7. Rollback: each capability is behind config defaults (cache disabled by default, guardrail falls back to Fast Guard, MCP adapter inactive without Tool policies, Agent/Task plumbing is additive metadata).

## Open Questions

- Should Exact Cache responses be stored pre- or post-Output-Guardrail for replay? Default: store the normalized upstream response and re-run Output Guardrail on every hit (frozen invariant); revisit if TTFT on cache hits needs tightening in Phase 3.
- Should the MCP Tool connector support Tasks extension in P2 or defer to Phase 4 A2A? Default: defer Tasks extension; P2 ships `server/discover` + `tools/call`.
