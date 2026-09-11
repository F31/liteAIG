## 1. RuntimeBundle Signing

- [x] 1.1 Add `internal/platform/bundle`: `RuntimeBundle` type (metadata + checksum + signature + snapshot), `Sign`/`Verify` with Ed25519 (stdlib), and a canonical payload builder.
- [x] 1.2 Ensure the bundle never embeds plaintext secrets (credentials remain `secret_ref`); add a test that inspects the serialized bundle for plaintext credential material.
- [x] 1.3 Add tests: sign/verify round-trip, tampered payload rejected, key mismatch rejected.

## 2. Prepare / ACK / NACK Activation

- [x] 2.1 Add `Prepare(bundle, validator)` returning ACK or NACK reason (schema, signature/checksum, references, local resources).
- [x] 2.2 Add `Activate` that atomically replaces the active snapshot only for ACKed bundles; a NACK keeps the prior version and reports `config_drift`.
- [x] 2.3 Add tests: valid bundle ACKed + activated, invalid bundle NACKed + drift + prior version retained, no partial activation.

## 3. Last Known Good Boot

- [x] 3.1 Add `internal/platform/lkg` store: `active.bundle`/`previous.bundle` per tenant written with temp+fsync+atomic rename; `LoadActive`/`LoadPrevious`/`Fallback`.
- [x] 3.2 Add `Boot` that prefers the Control Plane's current bundle and falls back to a signature-valid LKG when unreachable; corrupt `active` falls back to `previous`.
- [x] 3.3 Couple Data Plane readiness to bundle validity (not-ready for a tenant on a corrupt/invalid active bundle).
- [x] 3.4 Add tests: boot from LKG when Control Plane unreachable, corrupt-active fallback, readiness not-ready on invalid bundle.

## 4. Publish Wiring and Release Gates

- [x] 4.1 Wire the publish path to emit a signed bundle and the Data Plane to Prepare/ACK/NACK + LKG Save on activation.
- [x] 4.2 Run the full gate set (`go test -race ./...`, `go vet ./...`, architecture-test, `npm run check`, actionlint, gitleaks) and resolve blocking failures.
- [x] 4.3 Run `openspec validate --all --strict` and record Phase 4 DR/RuntimeBundle DoD evidence.
