# Roadmap — V8.6 spec-compliance batches (2026-09-04)

Status: **batches landed (Stage 7–15 implemented); this file is the historical
planning baseline**. The completion ledger and the remaining enterprise gaps
live in `docs/GAP_ANALYSIS.md`. This roadmap closes the delta between the frozen
V8.2/V8.5 specification (`docs/LiteAIG_AI_Gateway_V8.5.md`) and the
implemented Lite profile. It adds **no new product capability domains**
(V8.5 §0): every batch lands inside the existing seven-stage pipeline, the
three-layer service architecture, narrow capability interfaces, and the
One-Table-One-Owner / CI architecture guards.

Every gap below was verified against the code before planning (file:line
evidence in parentheses where noted).

## Cross-cutting rules for all batches

1. Data-plane capabilities only enter the fixed seven-stage pipeline
   (new checkpoints attach to existing stages).
2. Control-plane extensions use narrow capability interfaces + `Set*`
   slots on `ControlBackend` (V8.5 stage 6 pattern); no God Interface
   regression.
3. New tables require a declared owner in `architecture/table-owners.yaml`
   plus a `-- owner:` migration header (CI-enforced).
4. Per-batch verification gate: `go build` / `go vet` / `go test -race
   ./...` / `make check` (architecture boundaries) / dev-instance rebuild +
   smoke (readyz 200 / admin 401 / gateway 400) / Playwright management-loop
   e2e against a freshly built e2e binary.
5. The Lite profile (SQLite, single-tenant) stays first-class: every batch
   must work in it; Standard-tier items are marked "adapter ready / wiring
   pending".

## Batches

### Stage 7 — Function calling / structured output (P0)

- **Status**: ✅ Implemented. `interaction.ChatPayload.Tools/ToolChoice/ResponseFormat`,
  `Message.ToolCalls`, OpenAI `tool_calls`/`tool_choice`/`finish_reason`, Anthropic tool
  blocks, routing exclusion `tools_not_supported`, tool-call guardrail/DLP, cache
  exclusion, `tool_call_events` ledger.
- Spec: §5.1 P0-Commercial (tools / tool_choice, structured output),
  §18.3 Tool Governance, §3.6 Tool Call Guard.
- Gap (verified): `interaction.ChatPayload` has no `Tools/ToolChoice/
  ResponseFormat`; `Message` is `{Role, Content string}` only; neither
  protocol adapter decodes or encodes `tool_calls` / `tool_use`; streaming
  deltas carry no tool calls.
- Scope:
  - `kernel/interaction`: `ChatPayload.Tools/ToolChoice/ResponseFormat`,
    `Message.ToolCallID/ToolCalls`, response `Choice.FinishReason` +
    tool calls; extend `UnifiedRequest.ToolCalls()` (already gates caching).
  - OpenAI adapter: request decode (`tools`, `tool_choice`,
    `response_format`, `role:"tool"` history, assistant `tool_calls`),
    response encode (non-streaming + incremental `delta.tool_calls`),
    `finish_reason` pass-through.
  - Anthropic adapter: request decode (`tools`, `tool_choice`,
    `tool_result` blocks), response encode (`tool_use` content blocks,
    `stop_reason`, streaming `content_block_start/stop`).
  - Routing: new exclusion code `tools_not_supported` — applies only when
    a deployment declares a non-empty capability list lacking `tools`
    (undeclared capabilities keep working, no config breakage).
  - Guardrail: `role:"tool"` messages evaluated as untrusted tool results;
    tool-call arguments DLP-scanned (`block` enforced; `redact` on
    arguments escalates to block to keep JSON valid).
  - Cache: requests with tools are never cached (extends existing
    `ToolCalls()` gate); exact-cache key includes tool definitions.
  - Ledger: model-generated tool calls recorded to `tool_call_events`
    (existing table, owner `observability`) with request correlation.
  - `estimateContextTokens` counts tool-definition bytes.
- Files: `internal/kernel/interaction/types.go`,
  `internal/access/protocol/{openai,anthropic}/*`,
  `internal/routing/model/planner.go`, `internal/gateway/guardrail/handler.go`,
  `internal/gateway/cache/handler.go`, `internal/app/pipeline.go`,
  `internal/platform/storage/sqlrepo/toolcall.go` (usage only).

### Stage 8 — Token metering & TPM (P0)

- **Status**: ✅ Implemented. `UnifiedUsage` extended (cache read/write, cached input,
  reasoning, tool calls, `Source`/`Estimated`/`EstimationMethod`); tokenizer registry
  (`internal/tokenizer/registry.go`); TPM estimate-then-reconcile (`--gateway-tpm`);
  pre-request Context Guard (`internal/contextguard`).
- Spec: §12.1 (full `UnifiedUsage`), §12.2 (Tokenizer Registry),
  §12.3 (Context Guard), §14.5 (TPM estimate-then-reconcile).
- Gap (verified): `UnifiedUsage` only has `InputTokens/OutputTokens`;
  estimation is a flat chars/4 heuristic (`app/pipeline.go:600`); rate
  limiting is RPM-only (`policy/rate/limiter.go:10`).
- Scope: extend `UnifiedUsage` (cache read/write, cached input, reasoning,
  tool calls, `Source`, `Estimated`, `EstimationMethod`); tokenizer
  registry with provider-usage-first priority and versioned model→encoding
  map, conservative-estimate fallback; TPM limiter (pre-reserve by
  estimate, reconcile on final usage, reuse budget-ledger windows);
  pre-request Context Guard with strict-mode rejection and max-available-
  output computation.
- Storage: extend `usage_events` columns (owner `finops`); no new table.

### Stage 9 — Streaming three-tier guardrail wiring (P0)

- **Status**: ✅ Implemented. `pipeline.nFor` assembles Inline/Buffered/Shadow tiers into
  the stream collector; `stream_guardrail_e2e_test.go` covers injection cutoff,
  bounded buffer latency, clean termination.
- Spec: §3.6 (streaming MUST run the three-tier model), §16
  (Inline/Buffered/Shadow), §11.4 cache-safety invariants.
- Gap (verified): `guardrail/stream.go` implements all three tiers with
  tests, but `app/streamcollector.go` invokes none of them; streaming
  responses only get the post-assembly output check.
- Scope: wire `InlineStreamGuard` (rolling cross-chunk window) into
  `streamCollector.WriteChunk`; sentence/newline-boundary
  `BufferedStreamGuard` release; `ShadowGuard` async external check with
  late-violation stream cutoff and `retroactive` security events;
  invariant tests (injection cut at first violating chunk, bounded
  buffer latency, clean-stream termination).

### Stage 10 — Live routing metrics & health (P1)

- **Status**: ✅ Implemented. Rolling per-deployment metrics
  (`internal/app/routingmetrics.go`, `pipeline.recordRoutingMetrics`) feed
  `routing.Input.Metrics`/`Health`; `unhealthy` filter and `soft` scoring active;
  wiring covered by `routing_wiring_test.go`.
- Spec: §3.4 (Deployment Health / Circuit Filter, Soft Score), §11.3
  (cache_affinity soft input).
- Gap (verified): `routing.Input.Health` and `.Metrics` are never populated
  by the Lite pipeline (`app/pipeline.go:152-160`), so `unhealthy` can
  never fire and the `soft` strategy scores are flat (degenerate to ID
  order).
- Scope: rolling per-deployment metrics (latency/cost/load/success,
  1-minute process-local window) from the usage ledger into
  `routing.Input.Metrics`; health map from circuit state + provider probe
  (`providerProber` exists); expose `ScoreWeights` in the Config console;
  simulator/production parity tests.

### Stage 11 — RuntimeBundle delivery loop (P1, largest architectural item)

- **Status**: ✅ Implemented. Control signs on publish; split `mode=gateway` pulls the
  authenticated bundle endpoint, atomic activate; LKG keeps data plane serving.
  `internal/app/bundlesync.go`, `split_mode_e2e_test.go`, `lkg_boot_e2e_test.go`.
- Spec: §2.3.1 (signed RuntimeBundle), §2.3.2 (Prepare/ACK/NACK, atomic
  activate), §2.8 (Atomic Config invariant).
- Gap (verified): `internal/platform/bundle` has zero external call
  sites; split `mode=gateway`/`mode=control` cannot exchange config —
  LKG file fallback only.
- Scope: activation of the existing bundle code: control signs on publish,
  gateway pulls authenticated bundle endpoint, Prepare (schema/checksum/
  signature) → atomic Activate, NACK keeps LKG; cold-start full fetch for
  `mode=gateway`; split-mode e2e (2 gateways + control: publish → all
  activate → kill control → gateways keep serving); HA-gate subset
  (fault injection, rolling upgrade, bad-signature NACK).

### Stage 12 — Standard-tier coordination (P1)

- **Status**: ✅ Implemented. `--coordinator redis://` wiring for ledger/lease/inflight;
  DB-backed leader election (`coordination_leases`, owner `platform`); singleton
  sweeper/alert/push-outbox leases with stale takeover; two-gateway exactly-once smoke
  over Postgres+Redis (`internal/app/a2a_standard_process_smoke_test.go`).
- Spec: §2.2 (Lease/Leader Election for singleton tasks), §14.3
  (Reservation Sweeper), §14.4 (Redis unavailability semantics).
- Gap (verified): Redis adapters exist (`platform/coordination/redis*`)
  but are not wired into any profile; no leader election exists anywhere.
- Scope: `--coordinator redis://` wiring for ledger/lease/inflight;
  DB-backed lease election for sweeper/alert/migration singletons
  (new table `coordination_leases`, owner `platform`); Redis-failure
  degradation tests per §14.4.

### Stage 13 — Major private-cloud providers + prompt-cache awareness (P1)

- **Status**: ✅ Implemented. Bedrock (SigV4), Vertex (SA JWT), Azure OpenAI
  connectors are implemented behind `egress`. Prompt-cache usage is parsed
  (`cached_tokens`/`cache_read_input_tokens`) into `UnifiedUsage` cache fields with
  cache cost pricing. Anthropic outbound `cache_control` markers are passed from
  `interaction.Message.CacheControl` through `protocol.EncodeMessages` and are
  covered by connector-level request-body tests; OpenAI prompt-cache accounting is
  response-side because the compatible Chat API has no explicit request
  `cache_control` field.
- Spec: §1.5A (native mainstream providers), §11.3 (Provider Prompt Cache
  Awareness).
- Original gap (closed): provider coverage was limited to
  `openai|openai-compatible|anthropic` and prompt-cache request/usage handling
  was absent.
- Scope: Bedrock (SigV4), Vertex (SA JWT), Azure OpenAI (resource token)
  connectors behind the existing `egress` policy; Anthropic
  `cache_control` pass-through + OpenAI `cached_tokens` parsing into the
  Stage 8 `UnifiedUsage` cache fields; provider cost per cache pricing;
  real `cache_affinity` source for Stage 10.

### Stage 14 — Enterprise evidence & eventing (P2)

- **Status**: ✅ Implemented. Evidence Export (`GET /api/admin/evidence`, bodies/secrets
  excluded by construction); guardrail benchmark report artifact; webhook notifications
  (hot-reload, egress-validated, metadata-only); OTLP trace exporter (env-gated);
  Prometheus `/metrics`.
- Spec: §1.8.1 (versioned Guardrail Benchmark report), §1.8.2 (Evidence
  Export), Integrate list (Webhook, OTel, Prometheus).
- Scope: read-only Evidence Export (config/policy/epoch versions, RBAC
  assignments, audit, guardrail events, secret rotation, federation
  history; no prompt/response bodies or secrets by default); benchmark
  report as a CI/release artifact with regression delta; webhook
  notifications (alert/approval/guardrail events via `egress`); OTLP trace
  exporter (env-gated).

### Stage 15 — `/v1/responses` & multimodal (P2, last by design)

- **Status**: ✅ Implemented. Content-part `Message` (text/image) with vision
  encode/decode in OpenAI/Anthropic adapters; minimal stateless `POST /v1/responses`
  (+ `ResponsesPayload`), responses excluded from cache; guardrail on text parts.
- Spec: §5.1 P0-Commercial remainder, §5.2 (`KindResponses`, content
  parts).
- Why last: `Message.Content` becomes content parts (text/image), which
  touches the guardrail, connectors, and cache-key model — every earlier
  batch assumes the current text-only shape.
- Scope: content-part `Message` (text/image), vision encode/decode in both
  adapters, minimal stateless `/v1/responses` + `ResponsesPayload`,
  multimodal cache exclusion, guardrail on text parts only.

## Explicitly deferred (require a new product decision per §1.6)

The remaining deferred items and the full enterprise gap ledger are maintained
in `docs/GAP_ANALYSIS.md` (A-class = deferred, B-class = partial in implemented
areas, C-class = enterprise enhancements).

- image / audio / rerank / batch / Gemini-native (spec P1, no demand
  signal yet)
- MCP Tasks extension (spec P1)
- client SDKs (meaningless before Stage 7; OpenAI SDK base_url covers
  most integrations)
- full System→Tenant→Project→Agent policy inheritance (value appears with
  multi-tenant SaaS; sequence after Stage 12)
- multi-region active / DR drill automation (Enterprise tier, needs a real
  multi-AZ environment)

> Note: A2A Streaming / Push Notification was deferred in the original planning
> but has since been implemented (LiteAIG SSE profile + durable signed push
> outbox with exactly-once delivery, official SDK v1.1.2 interop) — see
> `docs/A2A_STREAMING.md`, `docs/A2A_TRUSTED_RELEASE.md`, `docs/A2A_CONFORMANCE.md`.

## Ordering rationale

```
Stage 7→9   product P0: what customers hit on day one (tools, TPM, stream safety)
Stage 10    make the "explainable routing" selling point real
Stage 11→12 keep the architecture honest (Standard-tier HA promises)
Stage 13    cost & coverage (private cloud, prompt-cache savings)
Stage 14    enterprise assurance (evidence, events, OTel)
Stage 15    interaction-model change, done last on purpose
```
