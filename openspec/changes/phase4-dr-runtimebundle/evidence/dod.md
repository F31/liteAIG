# Phase 4 — RuntimeBundle Signing + Last Known Good: DoD Evidence

Status: Complete (11/11 tasks)

This change delivers the V8.2 §2.3 delivery/HA foundation: signed immutable
RuntimeBundles (Ed25519), a Prepare/ACK/NACK activation handshake, and a
disk-persisted Last Known Good store with atomic writes and fallback boot.

## Capabilities Delivered

### RuntimeBundle Signing (`runtime-bundle-signing`)
- `internal/platform/bundle`: `RuntimeBundle` (metadata + payload checksum +
  Ed25519 signature + compiled snapshot), `Sign`/`Verify` over the canonical
  payload, and a no-plaintext-secret guarantee (credentials remain `secret_ref`).

### Prepare / ACK / NACK (`prepare-ack-nack-activation`)
- `Prepare(bundle, validator)` returns ACK or a NACK reason (schema,
  signature/checksum, references, local resources); `Activate` only accepts an
  ACKed bundle, preventing partial activation.

### Last Known Good (`last-known-good-boot`)
- `internal/platform/lkg`: `active.bundle`/`previous.bundle` per tenant written
  with temp+fsync+atomic rename; corrupt `active` falls back to `previous`;
  `Ready` couples Data Plane readiness to bundle validity.

## Gate Results

| Gate | Result |
| --- | --- |
| `go test -race -timeout 180s ./...` (Postgres 55432 + Redis 56379) | PASS |
| `go vet ./...` | PASS |
| `go run ./cmd/architecture-test` | PASS |
| `actionlint` | PASS |
| `gitleaks --config .gitleaks.toml detect` | no leaks found |
| `openspec validate --all --strict` | 16 passed, 0 failed |

## Tests

- Signing: round-trip verify, tampered payload rejected, key-mismatch rejected,
  no plaintext secret in the payload.
- Prepare/ACK/NACK: valid bundle ACKed + activated; tampered/incomplete/local-
  validator-violating bundles NACKed; NACKed bundle cannot activate.
- LKG: save/load, rotation to previous, corrupt-active fallback, boot from LKG
  when Control Plane is unreachable, readiness couples to bundle validity.

## Scope Note

The signing, Prepare/ACK/NACK, LKG store, and readiness coupling are fully
implemented and tested. Wiring the signed-bundle emission into the config
service publish path end-to-end (Control Plane signing + Data Plane Prepare/LKG
Save on activation) is the remaining integration; the primitives and their
contracts are in place. KMS-backed signing keys and Multi-AZ/Region DR
orchestration remain separate Phase 4 remainder items.
