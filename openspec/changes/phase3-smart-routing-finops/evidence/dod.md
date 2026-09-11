# Phase 3 — Smart Routing + FinOps: DoD Evidence

Status: Complete

This change delivered explainable smart routing (soft score + replay-only
Simulator), Enterprise FinOps (pricing/chargeback, multi-dimensional
aggregation, root-task/agent-hop rollup), the Agent resource model, suggest-only
Adaptive Recommendations, and the Guardrail Benchmark / Evidence Export. It
satisfies Scenario C (enterprise FinOps attribution) and the cost/latency-
optimized Scenario B.

## Capabilities Delivered

### Smart Routing Scoring (`smart-routing-scoring`)
- `Planner.order` gains a `soft` strategy: bounded normalized weighted sum over
  latency/cost/load/cache-affinity with a noise floor; score weights compiled
  from the Route Policy with defaults (`internal/routing/model/planner.go`).
- `CandidateEvidence` carries `ScoreBreakdown`; decisions are explainable.
- `Simulator` wraps `Planner.Plan` (replay-only) — `internal/routing/model/simulator.go`.
- Config `RoutePolicy.ScoreWeights` + snapshot cloning + defaults-preserve-
  priority regression tests.

### FinOps Pricing and Chargeback (`finops-pricing-chargeback`)
- `internal/finops/pricing`: `PriceVersion`, `Rate`, two-amount `Derive`
  (provider_cost vs customer_charge), `StaticConverter` multi-currency,
  attribution-qualified `Chargeback` (excludes untrusted labels).

### FinOps Attribution Aggregation (`finops-attribution-aggregation`)
- `internal/finops/aggregate`: `ByDimension` (multi-dimensional, derived from
  the ledger), `RootTask` rollup with per-hop breakdown, `QuantifyOptimizationCost`
  (cache savings / retry / fallback cost). `UsageRecord` gained `Cost`.

### Agent Resource Model (`agent-resource-model`)
- Config + snapshot `AgentEndpoint` (version, protocol, data classification,
  capabilities) and `Agent` version/capabilities; compiled `capabilityIndex`
  (`AgentsByCapability`) and version pinning.
- Agent version/endpoint persisted on usage facts (migrations 018/019,
  owner observability/finops).

### Adaptive Recommendations (`adaptive-governance-recommendation`)
- `internal/controlplane/recommend`: evidence-bounded, suggest-only
  recommendations; `Accept` creates a Config Draft via a `DraftCreator` and
  never mutates production.

### Guardrail Benchmark and Evidence Export (`guardrail-benchmark-evidence`)
- `internal/guardrail/benchmark`: versioned corpus harness with
  precision/recall/FP/FN by category/language and regression `Compare`;
  read-only `Exporter` that excludes prompt/response/secret by construction.

### Console
- Localized Routing Simulator tab in Governance (`/api/admin/simulator`,
  replay-only; zh-CN/en-US parity, no hard-coded strings).

## Gate Results

| Gate | Result |
| --- | --- |
| `go test -race -timeout 180s ./...` (Postgres 55432 + Redis 56379) | PASS |
| `go vet ./...` | PASS |
| `go run ./cmd/architecture-test` | PASS |
| `npm run check` (format, locales, hardcoded strings, vitest, build) | PASS |
| `actionlint` | PASS |
| `gitleaks --config .gitleaks.toml detect` | no leaks found |
| `openspec validate --all --strict` | 12 passed, 0 failed |

## Golden Scenario Evidence

1. **Scenario C** (`tests/golden/scenario_c_test.go`): Project × Department
   cross-analysis reconciles to the ledger; attribution trust distinguishes
   verified from untrusted; untrusted labels never produce User/OrgUnit
   chargeback; tenant-level chargeback totals reconcile with the ledger.
2. **Scenario B cost/latency-optimized** (`tests/golden/scenario_b_optimized_test.go`):
   cost-weighted soft score selects the cheaper deployment with per-component
   breakdown; the Simulator reproduces the production decision exactly; a cost
   recommendation is suggest-only and Accept creates a Draft without mutating
   production.
3. **Scenario D advanced**: Guardrail Benchmark is reproducible with regression
   deltas and no zero-FP/FN claims; Evidence Export excludes sensitive content.

## Scope Note

The full Phase 3 Console surface set (cross-analysis charts, Task/Agent Hop
cost pages, ranking, benchmark/evidence entry) is the remaining frontend work;
this change delivers the localized Routing Simulator surface plus all backend
modules and DoD-required golden assertions. The remaining Console pages are
tracked as follow-up within Phase 3, not blocking the backend DoD.
