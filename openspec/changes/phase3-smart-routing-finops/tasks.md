## 1. Smart Routing Scoring

- [x] 1.1 Extend `Planner.order` with a `soft` strategy: additive weighted sum over latency/cost/load/cache-affinity with bounded normalization and a noise floor; compile score weights from the Route Policy with defaults.
- [x] 1.2 Add score evidence to `CandidateEvidence` (per-component scores) and expose it on the Route Plan.
- [x] 1.3 Add a `Simulator` wrapping `Planner.Plan` that accepts replay inputs and overrides without mutating live state; prove simulator-vs-production decision equality in tests.
- [x] 1.4 Extend config types/validator/snapshot for Route Policy score weights; add defaults-preserve-priority regression tests.

## 2. FinOps Pricing and Chargeback

- [x] 2.1 Add `internal/finops/pricing` with Pricing Versions (rate lists) and two-amount derivation (provider_cost vs customer_charge); record the pricing version with the fact.
- [x] 2.2 Add multi-currency conversion with a validated configurable rate source; preserve original currency/amounts.
- [x] 2.3 Derive Chargeback/Showback from attribution-qualified usage only (exclude untrusted labels); support Tenant/Project/OrgUnit/User/Application/Agent/Task scopes.
- [x] 2.4 Add pricing golden tests, cross-currency tests, and untrusted-label-exclusion tests.

## 3. FinOps Attribution Aggregation

- [x] 3.1 Add `internal/finops/aggregate` deriving multi-dimensional rollups from the Usage Ledger (no separate fact source); browser receives aggregates only.
- [x] 3.2 Add Root Task / Agent Hop aggregation (root total = sum of descendants + hops) with per-hop breakdown.
- [x] 3.3 Quantify cache savings, retry cost, and fallback cost from usage facts.
- [x] 3.4 Add ledger-vs-aggregation reconciliation tests and cross-analysis (Project x Department) tests.

## 4. Agent Resource Model

- [x] 4.1 Add config + snapshot types for Agent Versions, Endpoints, and Capabilities; compile a capability index into the snapshot.
- [x] 4.2 Support task version pinning and capability filtering in resolution (snapshot-only hot path).
- [x] 4.3 Persist agent version/endpoint on usage facts so aggregation can group by version.
- [x] 4.4 Add tests: version-pinned task, capability index filtering, versioned usage attribution.

## 5. Adaptive Recommendations

- [x] 5.1 Add `internal/controlplane/recommend` generating routing/guardrail suggestions with evidence windows and explanation.
- [x] 5.2 Accept-as-Draft MUST create a Config Draft via the existing config service; dismiss is recorded and never mutates production.
- [x] 5.3 Add tests: accept creates draft only, dismiss leaves production unchanged, recommendation carries evidence window.

## 6. Guardrail Benchmark and Evidence Export

- [x] 6.1 Add a Guardrail Benchmark Harness (versioned corpus) reporting precision/recall/FP/FN by category/language with regression deltas; no zero-FP/FN promises.
- [x] 6.2 Add a read-only Evidence Export endpoint (authorized role, tenant/time range) that excludes prompt/response bodies and secrets by default.
- [x] 6.3 Add tests: benchmark reproducibility, regression delta, export excludes sensitive content.

## 7. Console and Release Gates

- [x] 7.1 Add localized Console surfaces: Routing Simulator, route score, Usage & Cost full, Project x Department cross analysis, Task/Root Agent/Agent Hop cost, Department/User ranking, Attribution Trust, Budget/Chargeback, Cost Anomaly, Pricing, Optimization Recommendations (Accept as Draft / Dismiss), Benchmark/Evidence entry — zh-CN/en-US parity.
- [x] 7.2 Add Scenario C golden assertions (cross-analysis accurate, attribution trust, dashboard-vs-ledger reconciliation, cache/retry/fallback cost queryable) and Scenario B cost/latency-optimized assertions (simulator equivalence, recommendation creates draft).
- [x] 7.3 Run the full gate set (`go test -race ./...`, `go vet ./...`, architecture-test, `npm run check`, actionlint, gitleaks) and resolve blocking failures.
- [x] 7.4 Run `openspec validate --all --strict` and record Phase 3 DoD evidence.
