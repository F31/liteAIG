# Phase 4 — Multi-AZ / Region DR

## Why

V8.2 §2.8/#30.11 require Standard/Enterprise HA first solved in-Region (≥2 AZ) and then Region DR with RuntimeBundle replication and a DR Runbook. `phase4-dr-runtimebundle` delivered the signed RuntimeBundle + local Last Known Good primitives; this change scales them to a Region-aware layout, couples DR readiness, and ships a testable DR Runbook checklist so RTO/RPO and failover are validated, not just drawn.

## What Changes

- Add a Region-scoped LKG layout: each Region owns its local `active.bundle`/`previous.bundle` store (reusing `internal/platform/lkg`), so one Region can boot its LKG while another is unreachable.
- Add DR readiness coupling: a Region is DR-ready when it has a signature-valid active bundle for a tenant and the node reports ready; a corrupt or invalid bundle makes DR readiness fail for that tenant.
- Add a Region DR Runbook checklist: a deterministic, importable set of DR steps (validate runtime, reconcile accounting/budget, switch traffic, verify readiness) with a pass/fail status per step, exposed as a read-only view.
- Wire the consistency modes (`regional/global_soft/global_hard`) and slice budget already delivered into the DR view so DR checks reflect the region's budget semantics.

## Capabilities

### New Capabilities
- `region-dr-layout`: Region-scoped LKG layout and DR readiness coupling.
- `dr-runbook-checklist`: deterministic, testable DR Runbook checklist and status view.

### Modified Capabilities
- `last-known-good-boot`: LKG boot and readiness are region-scoped and feed DR readiness.

## Impact

- **Backend**: new `internal/platform/lkg/region.go` (region-scoped store layout + DR readiness) and `internal/platform/dr` (runbook checklist + status); reuses `internal/platform/lkg`, `internal/app` readiness, and budget consistency modes.
- **Dependencies**: no new third-party runtime dependency.
- **Tests**: region isolation (one Region's corrupt bundle does not affect another), DR readiness coupling, runbook checklist steps with pass/fail, budget-mode-aware DR view, RTO/RPO representation.

### Non-Goals
- Real Kubernetes/Helm orchestration, actual multi-cluster failover automation, KMS-backed DR keys, gRPC Extension Bridge (P2), Phase 5 (separate items).

**Golden Scenario:** strengthens Scenario G (Region DR basics: LKG boot per region, DR readiness, runbook validation).