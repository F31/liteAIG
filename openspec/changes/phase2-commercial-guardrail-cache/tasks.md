## 1. Exact Cache

- [x] 1.1 Add the `Cache` contract in `internal/cache` with tenant/project-scoped keys (tenant, project, logical model, normalized request, semantic-affecting params, namespace version) and a Memory LRU store for Lite.
- [x] 1.2 Add a Valkey/Redis-backed cache store for Standard/Enterprise reusing the coordination client, with tenant/project namespace isolation and a cross-tenant-lookup-miss test.
- [x] 1.3 Wire the cache into Stage 3 Policy & Cost Preflight using the existing `SkipResolutionExecution` directive; on hit, re-run current Output Guardrail (Stage 6) and Accounting (Stage 7) on the cached response.
- [x] 1.4 Implement Cache Policy (cacheability rules, TTL, namespace version) compiled into the Tenant Runtime Snapshot with validation and defaults; default no-cache for tool calls, high temperature, Guardrail blocks, high-sensitivity PII/secret policy, and `Cache-Control: no-cache`.
- [x] 1.5 Add low-cardinality cache metrics (hit rate, savings, evictions) and cache namespace-version invalidation.
- [x] 1.6 Add tests: cache-hit-still-runs-output-guardrail, no cross-project sharing, tool-call-not-cached, no-cache directive, tenant-isolated store, and cache savings accounting.

## 2. Commercial Guardrail

- [x] 2.1 Extend the Guardrail Engine with configurable basic PII and Prompt Injection detectors alongside the existing Fast Guard, compiled from the snapshot, with block/redact actions and content-hash-only Security Events.
- [x] 2.2 Add a publishable Guardrail Policy model with Tighten/Loosen semantics, versioned Test Cases, a Guardrail-only Playground, and Impact Preview restricted to explicitly available samples.
- [x] 2.3 Add Guardrail Policy Fast Publish that updates policy and Security Epoch without full config publication, with audit of prior/new epoch.
- [x] 2.4 Add streaming Layer 1 (inline fast guard, per-chunk budget, cross-chunk rolling window, MASK/BLOCK) and Layer 2 (buffered local at sentence/newline/token window with bounded latency budget).
- [x] 2.5 Add the replaceable External Guardrail API contract with explicit fail modes (fail-open/fail-closed/bypass), timeout/failure metrics, and no per-chunk synchronous default.
- [x] 2.6 Add Security Event persistence (tenant-scoped, attributable to policy/rule/request/snapshot version, redacted) and tests: stream cross-chunk corpus, Layer 1/2 TTFT regression ≤20%, external guardrail timeout fail-mode observability, and no raw content in Security Events.

## 3. MCP Tool Governance

- [x] 3.1 Add the MCP 2026-07-28 Streamable HTTP adapter (`Mcp-Method`/`Mcp-Name`, `server/discover`) under `internal/access/protocol/mcp` and a Tool connector under `internal/connectors/tool` behind the existing `Invoker` contract.
- [x] 3.2 Add the Tool Registry/Catalog (servers, tools with schema, capability tags, data classification, endpoint) and Tool Policy/ACL compiled into the Tenant Runtime Snapshot.
- [x] 3.3 Enforce Tool ACL at the `TOOL_REQUEST` checkpoint in the pipeline with schema validation and DLP on arguments; reject before upstream invocation.
- [x] 3.4 Add Tool Result Content Provenance (`tool_result`, untrusted) with re-guardrail before use as context or response.
- [x] 3.5 Add Tool Call Events attributed to tenant/project/request/session/agent/user/tool with `tool_call_count` and cost, linked to task/request identifiers.
- [x] 3.6 Add tests: unauthorized tool call rejected, Prompt Injection cannot bypass ACL, schema-invalid call rejected, tool result re-guardrailed, tool call attribution, and tenant isolation for tools.

## 4. Agentic Identity Foundation

- [x] 4.1 Model Agent as `Principal.Type=agent` on the canonical Principal and add a minimal snapshot `AgentIndex`; verify no second identity subsystem via architecture CI.
- [x] 4.2 Plumb `task_id`, `root_task_id`, and `parent_task_id` through `accounting.Facts`, `RequestRecord`, `UsageRecord`, and OTel span attributes.
- [x] 4.3 Record session identifiers on request records for later agent-graph reconstruction (no graph yet).
- [x] 4.4 Add tests: agent principal resolution, task IDs preserved in accounting/usage/telemetry, session linkage, and no duplicate identity model.

## 5. Console and Configuration

- [x] 5.1 Add localized Guardrail Policy Editor, Test Cases, Guardrail-only Playground, Impact Preview, and Security Events surfaces with zh-CN/en-US JSON parity and no hard-coded strings.
- [x] 5.2 Add localized Cache Overview/Policies/Savings and MCP Servers/Tool Catalog/Tool Policy/Tool Calls surfaces.
- [x] 5.3 Enforce backend-authoritative permissions (including high-risk guardrail/tool actions requiring re-auth) and add hardcoded-string checks and secret-leak regression tests for new surfaces.

## 6. Release Gates (Scenario D basic + Scenario E partial)

- [x] 6.1 Add Scenario D basic assertions: Input/Output/Stream/Tool checkpoints recognize and act on samples, Guardrail decisions traceable to policy/rule/request, and Guardrail latency/False-Positive measurable.
- [x] 6.2 Add Scenario E partial assertions: unauthorized Tool Call rejected at `TOOL_REQUEST`, Tool Result re-guardrailed as untrusted, and Tool Call attributed/audited.
- [x] 6.3 Run the full gate set (`go test -race ./...`, `go vet ./...`, architecture-test, `npm run check`, actionlint, gitleaks) and resolve all blocking failures.
- [x] 6.4 Run `openspec validate --all --strict` and record Phase 2 DoD evidence in `openspec/changes/phase2-commercial-guardrail-cache/evidence/`.
