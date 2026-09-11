# Phase 4 — Multi-AZ / Region DR: Design

## Context

`internal/platform/lkg` persists `active.bundle`/`previous.bundle` per tenant on
one node's data directory with temp+fsync+atomic-rename and a `Ready` helper.
`internal/app/readiness.go` exposes `/readyz`. This change scopes the LKG layout
by Region and adds a deterministic DR Runbook checklist and DR readiness view,
reusing the bundle-signing and budget-consistency primitives from
`phase4-dr-runtimebundle` and `phase4-global-soft-budget`.

## Goals / Non-Goals

**Goals:**
- Region-scoped LKG layout so one Region can boot its LKG independently.
- DR readiness coupling (a Region is DR-ready only when it has a signature-valid
  active bundle for the tenant and the node is ready).
- A deterministic DR Runbook checklist (validate runtime, reconcile
  accounting/budget, switch traffic, verify readiness) with pass/fail status.
- Budget-consistency-mode-aware DR view.

**Non-Goals:**
- Real Kubernetes/Helm orchestration, multi-cluster failover automation, KMS-backed DR keys, gRPC Extension Bridge, Phase 5.

## Decisions

### 1. Region-scoped LKG layout
Wrap `lkg.Store` with a Region directory: `data/regions/<region>/<tenant>/...`.
Each Region opens its own store; one Region's corruption/availability never
affects another. Reuse the existing atomic-write and previous-fallback logic.
Alternative considered: a single global LKG. Rejected — Region isolation (a
corrupt bundle in one Region must not fail another) is the point.

### 2. DR readiness is bundle-driven
`internal/platform/lkg/region.go` exposes `RegionReady(ctx, region, tenant)` =
node ready AND the Region has a signature-valid active bundle. The existing
`Store.Ready` already covers the bundle half; the DR wrapper adds the region
scope and node-readiness coupling.

### 3. DR Runbook is a deterministic checklist
New `internal/platform/dr`: `Checklist` with ordered steps
(`ValidateRuntime`, `ReconcileAccounting`, `ReconcileBudget`, `SwitchTraffic`,
`VerifyReadiness`), each with `Execute(ctx, env) (StepResult, error)` producing a
pass/fail status + RTO/RPO hints. A `Run` executes them in order; a failure is
recorded and the checklist stops (fail-fast), reflecting a runbook that halts on
a failed preflight.
Alternative considered: a live multi-cluster switch. Rejected — the runbook is a
drill/validation artifact this slice can test; real failover automation is out
of scope.

### 4. Budget-mode-aware DR view
The DR view reports each Region's budget consistency mode (from
`phase4-global-soft-budget`) so a DR drill accounts for regional vs global
semantics.

## Risks / Trade-offs

- [Region isolation] → per-region store directories + tests.
- [Runbook false failure] → deterministic steps with explicit inputs; RTO/RPO are hints, not promises.
- [DR readiness too strict] → readiness requires a valid active bundle only for the tenant being failed over.

## Migration Plan

1. Add `internal/platform/lkg/region.go` (region-scoped store + RegionReady) with tests.
2. Add `internal/platform/dr` (checklist + runbook Run + status) with tests.
3. Wire the DR view (read-only) and budget-mode awareness.
4. Add a Scenario G strengthening assertion and run the full gate set; validate OpenSpec strict; record DoD evidence.

Rollback: region layout is a wrapper over the existing store; DR checklist is
read-only.