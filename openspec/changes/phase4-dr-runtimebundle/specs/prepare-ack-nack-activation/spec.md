# prepare-ack-nack-activation Specification

## ADDED Requirements

### Requirement: Prepare / ACK / NACK activation
The Data Plane SHALL Prepare a received bundle by validating schema compatibility, signature/checksum, references, and local resources. A valid bundle SHALL be ACKed and atomically activated; an invalid bundle SHALL be NACKed, the prior version SHALL remain active, and a `config_drift` SHALL be reported. A single node NACK MUST NOT load a partial bundle.

#### Scenario: Valid bundle ACKed and activated
- **WHEN** a Prepare-valid bundle is received
- **THEN** it is ACKed and atomically replaces the active snapshot
- **AND** no partial version is loaded

#### Scenario: Invalid bundle NACKed with drift
- **WHEN** a Prepare-invalid bundle is received
- **THEN** it is NACKed
- **AND** the node continues on the prior version and reports `config_drift`
