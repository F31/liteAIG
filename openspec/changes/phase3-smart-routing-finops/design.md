# Phase 3 — Smart Routing + FinOps: Design

## Context

Phase 2 left Resolution selecting among hard-constraint-eligible deployments by
priority/weighted/round-robin only (`internal/routing/model/planner.go:155`).
FinOps facts exist (`internal/finops/accounting.Facts`, the durable Spool, budget
reconcile, OrgUnit attribution) but there is no pricing, chargeback, aggregation,
or root-task/agent-hop accounting. Phase 3 turns routing into explainable
optimization (soft score + simulator) and FinOps into a chargeback-capable,
multi-dimensional ledger, closing Scenario C and the optimized Scenario B.

## Goals / Non-Goals

**Goals:**
- Soft Score (latency/cost/load/cache-affinity) over eligible deployments with
  stable normalization and snapshot-compiled weights; route decisions carry
  score evidence.
- A Routing Simulator that calls the same `PlanRoute()` as production and
  produces identical, explainable decisions.
- Pricing Versions, two amount sets (Provider Cost vs Customer Charge),
  multi-currency, and Chargeback/Showback.
- Multi-dimensional FinOps aggregation (Tenant/Project/OrgUnit/User/Application/
  Agent/Task) with Root Task / Agent Hop aggregation and quantified
  cache/retry/fallback cost.
- Agent Version/Endpoint/Capability data model compiled into the snapshot as a
  capability index.
- Strict Data Residency that cannot be bypassed.
- Suggest-only Adaptive Recommendation producing Config Drafts.
- Reproducible Guardrail Benchmark and read-only Evidence Export.

**Non-Goals:**
- Semantic Cache, OIDC/SAML/SCIM, ClickHouse sink, A2A adapter,
  Delegation/Approval, Federated Agent Trust, Multi-AZ/Region DR (Phase 4).

## Decisions

### 1. Soft score is an additive weighted sum with a noise floor
Extend `Planner.order` with a `soft` strategy: each eligible deployment is scored
as `Σ weight_i × normalized_i(metric)` where metrics (latency, cost, load,
cache-affinity) are normalized to [0,1] with a noise floor (e.g., `max(x,
floor)/floor` capped at 1) so small absolute differences do not dominate. Weights
are compiled from the snapshot Route Policy (new `ScoreWeights` map) with explicit
defaults. Hard constraints still run first; soft score only orders the eligible set.
Alternative considered: a black-box ML ranker. Rejected — V8.2 §8.8 requires
explainable decisions; weights are config, not learned model output.

### 2. Simulator shares the production planner
A `Simulator` wraps `Planner.Plan` and accepts replayed `Input`/traffic samples
plus overrides (weights, health, circuit). Because it calls the same function,
decisions are identical by construction; the console renders the full evidence
including per-component scores. No second route implementation.
Alternative considered: a standalone scoring engine. Rejected — divergence risk.

### 3. Pricing is versioned and separate from provider cost
`internal/finops/pricing` holds Pricing Versions (id, published_at, rate list
keyed by model/provider/currency) and a converter that produces two amounts from
Usage facts: `provider_cost` (from the ledger) and `customer_charge` (from the
price version). Multi-currency uses a configurable rate source; the ledger
records the settlement currency. Chargeback is a derived view over
attribution-qualified usage (verified/key_bound/delegated only for
User/OrgUnit/Agent chargeback).

### 4. Aggregation is a query layer over the Usage Ledger
`internal/finops/aggregate` builds tenant-scoped rollups from `usage_events`
(which already carry user/org/path/cost-center/attribution + task/session/agent
linkage from Phase 2). Root Task / Agent Hop aggregation sums descendant task
usage into the root and per-hop. Browser receives aggregates only; it never
accumulates raw usage detail (V8.2 §Phase 3 DoD).
Alternative considered: a separate rollup table. Rejected — V8.2 requires
Dashboard and Ledger SQL reconciliation to agree; deriving from the ledger keeps
one source of truth. An optional rollup can come later for scale.

### 5. Agent capability index is compiled
Extend the snapshot with Agent Version/Endpoint/Capability records compiled from
config (Workstream A). Resolution can then filter Agent endpoints by capability
(P2 Capability Routing groundwork) while the hot path stays snapshot-only.

### 6. Recommendations are suggest-only and land as Drafts
`internal/controlplane/recommend` produces suggestions (e.g., weight shift, cost
threshold) with an evidence window. `Accept` MUST create a Config Draft via the
existing `config.Service` path; there is no direct mutation of active config.
This preserves the frozen Draft/Publish boundary.

### 7. Benchmark and evidence are read-only exports
The Guardrail Benchmark Harness runs a versioned corpus through the builtin
engine and emits precision/recall/FP/FN by category/language; the Evidence Export
endpoint emits config/policy/epoch/RBAC/audit/guardrail/secret-rotation records
for a tenant/time range, defaulting to exclude prompt/response bodies and
secrets, gated by an authorized role.

## Risks / Trade-offs

- [Soft score weights mis-tuned] → explicit defaults + simulator comparison + adaptive recommendations are suggest-only.
- [Multi-currency rates stale] → rate source config with validation; ledger keeps settlement currency.
- [Aggregation vs ledger drift] → aggregation is a query over the ledger; reconciliation test asserts equality.
- [Recommendations over-applied] → accept only creates a Draft; no direct activation.
- [Data Residency bypass] → residency enforced in hard constraints (already) + strict test proves no bypass.

## Migration Plan

1. Add soft scoring + simulator; extend Route Policy config with score weights and validation.
2. Add Pricing Versions + chargeback derivation; add multi-currency rate source.
3. Add aggregation service + root-task/agent-hop rollup; add reconciliation test.
4. Add Agent Version/Endpoint/Capability config → snapshot index.
5. Add recommendation service + accept-as-draft; add benchmark harness + evidence export endpoint.
6. Add localized Console surfaces; run the full gate set; validate OpenSpec strict.

Rollback: each capability is behind config defaults; soft scoring falls back to
priority ordering when no score weights are set, and recommendation/benchmark
features are disabled by default.
