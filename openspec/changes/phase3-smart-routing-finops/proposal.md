# Phase 3 — Smart Routing + FinOps

## Why

Phase 2 delivered Exact Cache, Commercial Guardrail, and the Agentic foundation. Phase 3 delivers the two commercial differentiators that make spend and routing explainable and optimizable: Smart Routing with a soft score and a Simulator that reuses the production planner, and Enterprise FinOps with pricing/chargeback, multi-dimensional attribution, and root-task/agent-hop aggregation. It also adds the Agent resource data model (Workstream A Phase 3) and a suggest-only Adaptive Recommendation backend, closing Scenario C and the cost/latency-optimized Scenario B.

## What Changes

- Extend Resolution with a latency/cost/load/cache-aware Soft Score over hard-constraint-eligible deployments, with stable normalization (noise floor) and per-component weights compiled from the snapshot.
- Add a Routing Simulator that runs the same `PlanRoute()` implementation as production, so an operator can replay requests or benchmark traffic and get identical decisions with full explain output.
- Add Pricing Versions, two amount sets (Provider Cost vs Customer Charge), multi-currency conversion, and Chargeback/Showback derived from the Usage Ledger.
- Add multi-dimensional FinOps aggregation (Tenant/Project/OrgUnit/User/Application/Agent/Task), Root Task / Parent Task / Agent Hop usage aggregation, cache-savings/retry/fallback cost quantification, and budget/chargeback reconciliation with the Usage Ledger.
- Add the Agent Version / Endpoint / Capability data model compiled into the snapshot as a capability index (Workstream A), plus strict Data Residency enforcement that cannot be bypassed.
- Add an Adaptive Governance Recommendation backend (routing/guardrail) that is suggest-only: accepting a recommendation MUST create a Config Draft, never mutate production directly.
- Add a Guardrail Benchmark Harness and Evidence Export (read-only, default excludes prompt/response bodies and secrets).

## Capabilities

### New Capabilities
- `smart-routing-scoring`: soft score with stable normalization and weights, and a Routing Simulator sharing the production planner.
- `finops-pricing-chargeback`: pricing versions, two amount sets, multi-currency, and chargeback/showback.
- `finops-attribution-aggregation`: multi-dimensional FinOps aggregation including Root Task / Agent Hop and quantified cache/retry/fallback cost.
- `agent-resource-model`: Agent Version / Endpoint / Capability data model and snapshot capability index.
- `adaptive-governance-recommendation`: suggest-only recommendation backend that produces Config Drafts.
- `guardrail-benchmark-evidence`: reproducible guardrail benchmark harness and read-only evidence export.

### Modified Capabilities
- `basic-routing-resilience`: Resolution now selects by hard constraints then soft score; route decisions carry score evidence.

## Impact

- **Backend**: extend `routing/model/planner.go` with soft scoring + simulator entrypoint; new pricing/chargeback/aggregation services in `internal/finops`; Agent version/endpoint/capability types in the snapshot; recommendation service emitting Config Drafts; benchmark + evidence export under `internal/guardrail`/`internal/finops`.
- **APIs/Console**: Routing Simulator, route score, Usage & Cost full, Project×Department cross analysis, Task/Root Agent/Agent Hop cost, Department/User ranking, Attribution Trust, Budget/Chargeback, Cost Anomaly, Pricing, Optimization Recommendations (Accept as Draft / Dismiss), Guardrail Benchmark / Evidence Export entry — all localized.
- **Dependencies**: no new third-party runtime dependencies; multi-currency uses a configurable rate source.
- **Tests**: simulator-vs-production equivalence, pricing golden, budget overrun SLO, Data Residency non-bypass, Ledger-vs-aggregation reconciliation, and recommendation-must-create-draft.

### Non-Goals
- Semantic Cache, OIDC/SAML/SCIM, ClickHouse sink, A2A adapter, Delegation/Approval, Federated Agent Trust, Multi-AZ/Region DR (Phase 4).

**Golden Scenarios:** Scenario C (enterprise FinOps attribution, complete) and Scenario B cost/latency-optimized; Scenario D advanced guardrail via Benchmark/Evidence.
