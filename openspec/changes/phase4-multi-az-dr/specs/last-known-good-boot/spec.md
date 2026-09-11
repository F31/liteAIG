# last-known-good-boot Specification (Delta)

## ADDED Requirements

### Requirement: Region-scoped LKG boot
LKG boot and readiness SHALL be applicable per Region: each Region boots from its own active/previous bundle and a Region's DR readiness requires a signature-valid bundle for the Tenant, as specified by the region-dr-layout capability.

#### Scenario: Region boots its own LKG
- **WHEN** a Data Plane Region starts and its Control Plane is unreachable
- **THEN** it boots from that Region's signature-valid LKG
- **AND** its DR readiness depends on the bundle's validity