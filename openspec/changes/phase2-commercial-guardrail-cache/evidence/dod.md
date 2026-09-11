# Phase 2 — P0-Commercial Guardrail + Cache: DoD Evidence

Status: Complete

This change delivered Exact Cache, Commercial Guardrail, MCP Tool Governance, and
the Agentic Identity Foundation, and satisfies the Scenario D basic + Scenario E
partial gates.

## Capabilities Delivered

### Exact Cache
- `internal/cache`: tenant/project-scoped `Key` (SHA-256, includes namespace
  version), `Entry`, `Store`, `Stats`; Memory LRU (`memory.go`) and
  Valkey/Redis store (`redis.go`).
- `internal/gateway/cache`: `Cacheable` rules (allowed kinds, tool-call
  exclusion, temperature gate, `no-cache` directive), Stage 3 `Handler` that
  returns `SkipResolutionExecution` on hit and re-runs Output Guardrail +
  Accounting on the cached response, and `WriteBack` after Accounting.
- Cache Policy compiled into `TenantRuntimeSnapshot.CachePolicy()` with
  validation (TTL/namespace/temperature non-negative).

### Commercial Guardrail
- `internal/guardrail/builtin`: added `pii` and `prompt_injection` rule kinds.
- `internal/guardrail`: publishable `Policy` with versioned Test Cases
  (`RunTests`), Impact Preview over explicit samples only, Tighten/Loosen Fast
  Publish with Security Epoch + audit hook (`PolicyRegistry`), streaming Layer 1
  (`InlineStreamGuard`, cross-chunk rolling window) and Layer 2
  (`BufferedStreamGuard`, sentence/newline/token release), replaceable External
  Guardrail API with explicit fail-open/fail-closed/bypass modes.
- Security Events: tenant-scoped persistence (`security_events` table, owner
  guardrail, RLS-enabled), redacted content-hash only, wired into the guardrail
  handler.

### MCP Tool Governance
- `internal/access/protocol/mcp`: MCP 2026-07-28 normalization (`server/discover`,
  `tools/call`).
- `internal/connectors/tool/mcp`: Streamable HTTP connector with Discover/Invoke,
  `Mcp-Method`/`Mcp-Name` headers, Tool Call attribution event, normalized
  upstream errors.
- `internal/gateway/tool`: `TOOL_REQUEST` ACL (non-bypassable by Prompt
  Injection), JSON-schema required-field validation, and DLP on arguments.
- Tool Result carried with `Provenance{Source: tool_result, Trusted: false}` and
  re-guardrailed at the output checkpoint.
- Tool Registry/Catalog + Tool Policy/ACL compiled into the snapshot.

### Agentic Identity Foundation
- Canonical `Principal.Type=agent` (`identity.PrincipalAgent`) and snapshot
  `AgentIndex`.
- `session_id/task_id/root_task_id/parent_task_id/agent_id` plumbed through
  Admission → `InteractionContext` → `accounting.Facts` → `request_records` +
  `usage_events` (owner-split migrations 014/015) → OTel span attributes.

### Console
- New localized `Governance` page (Guardrail editor + Fast Publish with re-auth,
  Test/Playground/Impact Preview/Security Events, Cache overview, MCP
  Servers/Tool Catalog/Policy/Calls, Integrations) in `governance.json` with
  zh-CN/en-US parity.
- Backend: `GET /api/admin/governance` (read-only view) and
  `POST /api/admin/guardrail/publish` requiring `X-Reauth-Token` (high-risk
  action).

## Gate Results

| Gate | Result |
| --- | --- |
| `go test -race -timeout 180s ./...` (Postgres 55432 + Redis 56379) | PASS (all packages) |
| `go vet ./...` | PASS |
| `go run ./cmd/architecture-test` | PASS (`architecture boundaries: ok`) |
| `npm run check` (format, locales, hardcoded strings, vitest, build) | PASS |
| `actionlint` | PASS |
| `gitleaks --config .gitleaks.toml detect` | no leaks found |
| `openspec validate --all --strict` | 10 passed, 0 failed |

## Golden Scenario Evidence

1. **Scenario D basic** (`tests/golden/scenario_d_e_test.go`): Input checkpoint
   blocks Prompt Injection and redacts PII with content-hash-only Security
   Events; Output checkpoint re-guardrails response content; decisions are
   traceable to rule/request and carry no raw bodies.
2. **Scenario E partial**: unauthorized Tool Call rejected at `TOOL_REQUEST`
   before upstream invocation; Tool Result re-guardrailed as untrusted
   `tool_result` provenance; Tool Call attribution events carry tenant/project/
   request/session/task/agent/user.

## Sensitive-Data Notes

- Security Events and Impact Previews never include prompt, response, or secret
  material; the `security_events` schema has no prompt/response columns.
- Guardrail Fast Publish requires a re-authentication token that is used only
  for the one request and never persisted.
- Tool arguments/results are never written to logs or telemetry verbatim;
  Tool Call events carry identities and identifiers only.
