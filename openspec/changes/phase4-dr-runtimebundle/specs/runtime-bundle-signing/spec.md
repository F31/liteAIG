# runtime-bundle-signing Specification

## ADDED Requirements

### Requirement: Signed immutable runtime bundles
Published configuration SHALL be delivered as an immutable RuntimeBundle carrying `SchemaVersion`, `TenantID`, `TenantRef`, `ConfigVersion`, `SecurityEpoch`, `SystemRuntimeVersion`, `PublishedAt`, `PayloadChecksum`, `Signature`, and the compiled `Snapshot`. The payload SHALL be signed with Ed25519, and the Data Plane SHALL verify the signature before loading. The bundle SHALL NOT contain plaintext secrets; credentials remain opaque `secret_ref` values.

#### Scenario: Tampered bundle rejected
- **WHEN** a bundle payload is modified after signing
- **THEN** signature verification fails
- **AND** the bundle is not loaded

#### Scenario: Bundle carries no plaintext secret
- **WHEN** a bundle is inspected
- **THEN** no plaintext credential material is present
- **AND** credentials are referenced by `secret_ref`
