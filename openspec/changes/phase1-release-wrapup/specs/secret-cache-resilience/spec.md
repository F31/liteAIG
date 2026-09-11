# secret-cache-resilience Specification

## ADDED Requirements

### Requirement: Replaceable Secret Provider
Credential resolution SHALL go through a replaceable Secret Provider abstraction that resolves opaque `secret_ref` values to material server-side. Domain and connector code SHALL depend on the provider contract, not on KMS/Vault SDKs, so a Secret Provider outage does not put the Secret Provider on every request's synchronous hot path.

#### Scenario: Credential resolved through provider contract
- **WHEN** a connector needs a credential for an upstream call
- **THEN** it resolves the credential through the Secret Provider using its `secret_ref`
- **AND** no raw secret is embedded in the runtime snapshot or request path

### Requirement: Credential cache with TTL and stale grace
The Secret Provider SHALL cache decrypted credentials in memory governed by `secret_cache_ttl`, `secret_stale_grace`, and `rotation_overlap`. A credential that was successfully decrypted and is still within TTL/grace SHALL remain usable when the Secret Provider is temporarily unavailable. New decryption, first use, and rotation SHALL always require the Secret Provider.

#### Scenario: Provider outage within grace serves cached credential
- **WHEN** the Secret Provider becomes unavailable but a credential is cached and within its stale grace window
- **THEN** the cached credential continues to be used for existing calls
- **AND** an outage alert is raised

#### Scenario: Rotation requires the provider
- **WHEN** a credential rotation overlaps an active credential
- **THEN** the new material is decrypted from the Secret Provider before use
- **AND** the cache is refreshed with the rotated value

### Requirement: Fail-closed after grace
After `stale_grace` expires, credential resolution SHALL fail closed according to the credential's policy. The system MUST NOT use stale secrets indefinitely during a Secret Provider outage.

#### Scenario: Grace expiry blocks stale use
- **WHEN** a credential's cached value exceeds TTL plus stale grace and the Secret Provider is still unavailable
- **THEN** resolution fails closed for that credential
- **AND** the failing state is observable and alerted

### Requirement: Secret cache isolation and no-leak guarantees
The in-memory credential cache SHALL be node-local and SHALL NOT persist secret material to disk, URLs, logs, telemetry, diffs, audit payloads, or any browser-visible or cross-tenant response. Any locally persisted secret cache, if enabled, SHALL use node-key/Envelope Encryption and remain separate from the RuntimeBundle.

#### Scenario: No secret in API or observability surfaces
- **WHEN** a Secret Provider outage occurs and alerts or health surfaces are emitted
- **THEN** no plaintext secret appears in alerts, logs, traces, metrics, diffs, or admin API responses
- **AND** cross-tenant resolution of a secret_ref returns a non-disclosing error
