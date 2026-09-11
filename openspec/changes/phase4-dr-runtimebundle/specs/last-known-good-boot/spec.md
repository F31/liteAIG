# last-known-good-boot Specification

## ADDED Requirements

### Requirement: Local Last Known Good persistence
Each Data Plane SHALL persist `active.bundle` and `previous.bundle` per Tenant on local disk, written with temp file + fsync + atomic rename. Boot SHALL prefer the Control Plane's current bundle and SHALL fall back to a signature-valid LKG when the Control Plane is unreachable. A corrupt `active.bundle` SHALL fall back to `previous.bundle`.

#### Scenario: Boot from LKG when Control Plane unreachable
- **WHEN** a Data Plane starts and the Control Plane is unreachable
- **THEN** it boots from a signature-valid local LKG
- **AND** it serves the allowed configuration

#### Scenario: Corrupt active falls back to previous
- **WHEN** `active.bundle` is corrupt
- **THEN** the node falls back to `previous.bundle`
- **AND** the fallback is observable

### Requirement: Readiness couples to bundle validity
Data Plane readiness SHALL require at least one valid bundle loaded. A corrupt or signature-invalid active bundle SHALL make the node not-ready for that Tenant while it continues serving in-flight requests on the loaded snapshot.

#### Scenario: Not ready on invalid bundle
- **WHEN** the active bundle fails verification
- **THEN** readiness reports not-ready for that Tenant
- **AND** in-flight requests on the prior loaded snapshot continue
