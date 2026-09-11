# scim-user-provisioning Specification

## ADDED Requirements

### Requirement: User provisioning contract
The system SHALL provide a SCIM provisioning contract (`UpsertUser`, `DeactivateUser`) so external IdP directory changes can be applied, with a deterministic mock for tests. A full SCIM REST server is out of scope for this slice.

#### Scenario: Mock provisioning
- **WHEN** a user is upserted through the contract
- **THEN** the provisioner records the change deterministically
- **AND** the record is attributable to the provisioning source
