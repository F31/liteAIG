# Phase 4 — RuntimeBundle Signing + Last Known Good

## Why

V8.2 §2.3.1/2.3.2/2.3.3 require Standard/Enterprise RuntimeBundles to be signed, distributed with Prepare/ACK/NACK, and persisted locally as Last Known Good (LKG) so a Data Plane can boot from a signed bundle when the Control Plane is unreachable. Today the compiler emits a `TenantRuntimeSnapshot` with checksum + atomic activate, but there is no signature, no Prepare/ACK/NACK handshake, and no disk-persisted LKG with temp+fsync+atomic-rename. This is the DR/HA foundation every Phase 4 Multi-AZ/Region-DR item depends on, and it is fully testable.

## What Changes

- Add a `RuntimeBundle` type with `SchemaVersion`, `TenantID`, `TenantRef`, `ConfigVersion`, `SecurityEpoch`, `SystemRuntimeVersion`, `PublishedAt`, `PayloadChecksum`, `Signature`, and the compiled `Snapshot`.
- Sign the bundle payload with Ed25519 (a `Signer`/`Verifier` contract); Standard/Enterprise verify the signature before loading. Secret material stays behind `secret_ref` — the bundle carries no plaintext secrets.
- Add a Prepare/ACK/NACK handshake: Data Plane validates schema compatibility, signature/checksum, references, and local resources; it ACKs a valid bundle and NACKs an invalid one, continuing to run the prior version and reporting `config_drift` on NACK.
- Add an LKG store on the Data Plane: `active.bundle`/`previous.bundle` per tenant written with temp+fsync+atomic rename; boot prefers the Control Plane's current version and falls back to a signature-valid LKG when unreachable; corrupt `active` falls back to `previous`.
- Couple readiness to bundle validity (a signature-invalid or corrupt active bundle makes the node not-ready for that tenant).

## Capabilities

### New Capabilities
- `runtime-bundle-signing`: signed immutable RuntimeBundles with Ed25519 verification.
- `prepare-ack-nack-activation`: Prepare/ACK/NACK handshake with atomic activation and config-drift reporting on NACK.
- `last-known-good-boot`: disk-persisted LKG (temp+fsync+atomic rename) with fallback boot and readiness coupling.

### Modified Capabilities
- `versioned-config-delivery`: published configuration is now delivered as a signed RuntimeBundle with Prepare/ACK/NACK and LKG persistence.

## Impact

- **Backend**: new `internal/platform/bundle` (bundle type, sign/verify, checksum), `internal/platform/lkg` (temp+fsync+atomic-rename store, boot fallback), and Prepare/ACK/NACK wiring in the config compiler/publish path; readiness coupling in `internal/app`/readiness.
- **Dependencies**: Ed25519 from the standard library; no new third-party dependency.
- **Tests**: signature verification (tamper detection), Prepare/ACK/NACK (valid ACK, invalid NACK + drift), LKG boot when Control Plane unreachable, corrupt-active falls back to previous, readiness not-ready on invalid bundle, no plaintext secret in the bundle.

### Non-Goals
- Multi-AZ/Region DR orchestration, DR Runbook, KMS-backed signing keys, OIDC/SAML/SCIM, ClickHouse sink (separate Phase 4 remainder items).

**Golden Scenario:** strengthens Scenario G (LKG boot when Control Plane is down; config never half-activated).
