# region-dr-layout Specification

## ADDED Requirements

### Requirement: Region-scoped LKG
Each Region SHALL own its local Last Known Good store (active/previous bundle per tenant) so one Region can boot independently while another is unreachable. A corrupt or invalid bundle in one Region SHALL NOT affect another Region.

#### Scenario: Region isolation
- **WHEN** one Region's active bundle is corrupt
- **THEN** that Region falls back to its previous bundle
- **AND** another Region's bundles remain usable

### Requirement: DR readiness coupling
A Region SHALL be DR-ready for a Tenant only when the node is ready AND the Region has a signature-valid active bundle for that Tenant. A corrupt or invalid bundle SHALL make DR readiness fail for that Tenant.

#### Scenario: Not DR-ready on invalid bundle
- **WHEN** the active bundle for a Tenant fails verification in a Region
- **THEN** DR readiness for that Tenant is false
- **AND** the failure is observable