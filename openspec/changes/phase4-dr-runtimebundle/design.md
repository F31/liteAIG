# Phase 4 — RuntimeBundle Signing + Last Known Good: Design

## Context

The config compiler (`internal/controlplane/config`) produces a
`TenantRuntimeSnapshot` with `Version` + `PublishedAt` and the Control Plane
activates it atomically via `registry.ActivateTenant`. There is no signed
bundle, no Prepare/ACK/NACK handshake, and no disk-persisted Last Known Good. The
app readiness path (`internal/app/readiness.go`) reports ready when a lifecycle
is running, independent of bundle validity. This change adds the V8.2 §2.3
delivery/HA foundation on top of the existing snapshot.

## Goals / Non-Goals

**Goals:**
- A signed `RuntimeBundle` with Ed25519 verification.
- Prepare/ACK/NACK activation; invalid bundles are NACKed and never replace the
  active version; a NACK reports `config_drift`.
- Disk-persisted LKG (`active.bundle`/`previous.bundle`) written with
  temp+fsync+atomic-rename; boot falls back to a signature-valid LKG when the
  Control Plane is unreachable.
- Readiness couples to bundle validity.

**Non-Goals:**
- Multi-AZ/Region DR orchestration, DR Runbook, KMS-backed signing, OIDC/SAML/
  SCIM, ClickHouse sink.

## Decisions

### 1. Bundle is a signed envelope over the compiled snapshot
New `internal/platform/bundle`: `RuntimeBundle` holds metadata + `PayloadChecksum`
+ `Signature` + `Snapshot *runtime.TenantRuntimeSnapshot`. Signing uses Ed25519
(standard library) over the canonical payload; `Verify` rejects tampered or
re-signed payloads. Secrets are never embedded — credentials remain `secret_ref`
in the snapshot.
Alternative considered: HMAC with a shared key. Rejected — asymmetric signing
allows Control-Plane-only signing and Data-Plane-only verification.

### 2. Prepare/ACK/NACK is a pure function over the bundle
`Prepare(bundle, localResourceValidator)` returns ACK or a NACK reason by
checking schema compatibility, signature/checksum, references (providers,
deployments, credentials exist), and local resource constraints. `Activate` is
atomic: only an ACKed bundle replaces the active snapshot; a NACK keeps the prior
version and surfaces `config_drift`. No partial activation is possible.
Alternative considered: activating on receipt. Rejected — V8.2 §2.3.2 requires
validation before activation.

### 3. LKG is a local file store with atomic writes
New `internal/platform/lkg`: `Store` with `Save(active bundle)`,
`LoadActive()`, `LoadPrevious()`, and `Fallback()`. Writes use temp file +
fsync + atomic rename; `active.bundle` corruption falls back to
`previous.bundle`. `Boot` prefers the Control Plane's current bundle, else a
signature-valid LKG; only a valid bundle is loaded.
Alternative considered: relying on the DB. Rejected — LKG must boot the Data
Plane without Control Plane/DB (§2.3.3).

### 4. Readiness couples to bundle validity
`internal/app` readiness requires at least one valid bundle loaded; a corrupt or
signature-invalid active bundle makes the node not-ready for that tenant while
still serving in-flight requests on the loaded snapshot.

## Risks / Trade-offs

- [Signing key rotation] → versioned keys; a bundle verifies under the key active at publish.
- [LKG disk corruption] → temp+fsync+atomic rename + previous.bundle fallback + readiness gating.
- [NACK leaves old version] → config_drift surfaced; node continues on prior version.

## Migration Plan

1. Add `internal/platform/bundle` (type, sign/verify, checksum, Prepare/ACK/NACK) with tests.
2. Add `internal/platform/lkg` store (temp+fsync+atomic rename, active/previous, boot fallback) with tests.
3. Wire the publish path to emit a signed bundle and the Data Plane to Prepare/ACK/NACK + LKG Save.
4. Couple readiness to bundle validity.
5. Run full gate set + OpenSpec strict validation + record DoD evidence.

Rollback: bundle signing is enabled only when a signing key is configured; without
it, the existing checksum+activate path remains.
