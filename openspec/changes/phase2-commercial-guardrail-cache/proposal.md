# Phase 2 — P0-Commercial Guardrail + Cache

## Why

Phase 0 delivered the Fast Guard (`builtin.Engine`: keyword/regex/secret with block/redact, already wired to Stage 2 and Stage 6 in `internal/gateway/guardrail/handler.go`), and the seven-stage pipeline already reserves a cache-compatible short circuit (`Directive.SkipResolutionExecution`) that no real cache implements. Phase 2 turns guardrails from "a filter" into a governed lifecycle and delivers Exact Cache — the two prerequisites for commercial adoption and for the V8.2 Phase 3+ FinOps/routing work. It also lays the Agentic foundation (Agent Identity on the canonical Principal, MCP Tool governance) required by the V8.2 Federated Agent Trust program.

## What Changes

- Add an Exact Cache with a tenant-scoped Cache Policy compiled into the snapshot; cache hits short-circuit Resolution/Execution via the existing pipeline directive but MUST still run the current Output Guardrail and Accounting. Cache savings are measurable.
- Extend the Guardrail Engine from deterministic Fast Guard to a commercial guardrail lifecycle: basic PII and Prompt Injection detection, explicit input/output Guardrail execution, streaming Layer 1 (inline fast) and Layer 2 (buffered local), a replaceable External Guardrail API with its own fail modes, a Security Event record, and Guardrail Policy Fast Publish (Tighten/Loosen) that does not require full config publication.
- Add MCP 2026-07-28 Tool governance: stateless Streamable HTTP adapter, `server/discover`, Tool Registry/Catalog, Tool ACL, schema/DLP checks, Content Provenance, and Tool Call Events tied to the request/session.
- Establish Agentic foundation: Agent Identity as a type of the canonical Principal (no second identity subsystem), and Task ID plumbing so `task_id`/`root_task_id`/`parent_task_id` propagate through Request Explorer, Accounting, and Telemetry.
- Add localized Console surfaces: Guardrail Policy Editor, Guardrail Test Cases, Guardrail-only Playground, Impact Preview (only explicitly available samples), Security Events, Cache Overview/Policies/Savings, and MCP Servers / Tool Catalog / Tool Policy / Tool Calls basics.

## Capabilities

### New Capabilities
- `exact-cache`: Exact key/value cache with tenant-scoped Cache Policy, pipeline short-circuit honoring Output Guardrail and Accounting, and cache savings metrics.
- `commercial-guardrail`: PII/Prompt-Injection detection, input/output guardrails, streaming Layer 1/2, External Guardrail API with fail modes, Security Events, and Guardrail Policy Fast Publish (Tighten/Loosen).
- `mcp-tool-governance`: MCP 2026-07-28 Tool adapter, Tool Registry/Catalog, Tool ACL, schema/DLP checks, Content Provenance, and Tool Call Events.
- `agentic-identity-foundation`: Agent Identity on the canonical Principal model and Task ID plumbing across Request Explorer, Accounting, and Telemetry.

### Modified Capabilities
- None. All Phase 2 behavior is additive; the existing `modular-runtime-foundation` cache-short-circuit requirement is fulfilled rather than changed.

## Impact

- **Backend**: new cache store/contract under `internal/cache` wired into the Policy & Cost Preflight stage; Guardrail engine extension in `internal/guardrail` (PII/Prompt-Injection detectors, streaming Layer 1/2, External Guardrail contract, Security Event, Fast Publish); new MCP adapter under `internal/access/protocol/mcp` and `internal/connectors/tool`; Agent Identity fields on `identity.Principal` (already carries `AgentID`) and snapshot `AgentIndex`; `task_id` propagation in `accounting.Facts`, Request Explorer, and OTel spans.
- **APIs/Console**: Guardrail/Cache/MCP/Tool surfaces in `web/console` with zh-CN/en-US JSON parity and no hard-coded strings; backend-authoritative permissions (incl. high-risk actions requiring re-auth).
- **Dependencies**: no new third-party runtime dependencies; External Guardrail and Semantic Cache remain Integrate options (Semantic Cache is Phase 4).
- **Tests**: stream cross-chunk corpus; cache-hit-still-guards; Layer 1/2 TTFT regression ≤20%; External Guardrail timeout/fail-mode observability; Tool ACL non-bypass by Prompt Injection; tenant isolation for cache; secret-leak regression for Security Events.

### Non-Goals
- Semantic Cache (Phase 4), OIDC/SAML/SCIM, ClickHouse sink, A2A adapter, Delegation/Approval, and Federated Agent Trust (Phase 4).
- Guardrail LLM-as-Judge, advanced DLP, Groundedness (Phase 4); Capability Routing (Phase 5).

**Golden Scenarios:** maps to Scenario D (enterprise AI security governance, basic) and Scenario E partial (unauthorized Tool Call rejected at `TOOL_REQUEST`, Tool Result re-guardrailed, Tool Call attribution/audit).
