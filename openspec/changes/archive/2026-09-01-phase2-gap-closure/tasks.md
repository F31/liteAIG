## 1. Guardrail Policy Persistence

- [x] 1.1 Add `guardrail_policies` migration (owner guardrail): id, tenant_id, version, security_epoch, change_type, rules JSON, actor, published_at.
- [x] 1.2 Add `sqlrepo.GuardrailPolicyStore` with tenant-scoped GetActive/Create and tests proving persistence and cross-tenant isolation.
- [x] 1.3 Extend `PolicyRegistry.FastPublish` to persist via the store and record an audit event (prior/new epoch); add startup load of the active policy.
- [x] 1.4 Compile the active guardrail policy into `TenantRuntimeSnapshot` (config `GuardrailPolicy` → snapshot) and expose `GuardrailPolicy()`.
- [x] 1.5 Update `gateway/guardrail` to build its engine from the snapshot policy; add tests: tighten effective at runtime, in-flight keeps captured version.
- [x] 1.6 Surface the active Security Epoch on the Governance/Health view.

## 2. Streaming Shadow Guard

- [x] 2.1 Add `ShadowGuard` in `guardrail/stream.go`: async External evaluation with per-request stop channel, never on the delivery path.
- [x] 2.2 On a late violation, stop further output and record `BLOCK_RETROACTIVE` Security Event.
- [x] 2.3 Add tests: retroactive block disposition, shadow guard off the hot path.

## 3. Observability Wiring

- [x] 3.1 Add `MetricSinkAdapter` implementing `cache.Metrics` and `accounting.SpoolMetrics` over `observability.MetricSink`.
- [x] 3.2 Verify cache/spool metrics reach the sink via a test.

## 4. Tool Call and Security Event Surfaces

- [x] 4.1 Add `tool_call_events` migration (owner observability) and `sqlrepo.ToolCallStore`; MCP connector persists `tool.call` events.
- [x] 4.2 Add tenant-scoped `GET /api/admin/security-events` and `GET /api/admin/tool-calls` endpoints.
- [x] 4.3 Render real Security Event and Tool Call lists in the Governance Console (zh-CN/en-US parity, no hard-coded strings).

## 5. Runtime Assembly and Release Gates

- [x] 5.1 Add a pipeline assembly contract test composing all seven stages + cache + tool + guardrail handlers and proving a request flows end to end.
- [x] 5.2 Run the full gate set (`go test -race ./...`, `go vet ./...`, architecture-test, `npm run check`, actionlint, gitleaks) and resolve blocking failures.
- [x] 5.3 Run `openspec validate --all --strict` and record DoD evidence.
