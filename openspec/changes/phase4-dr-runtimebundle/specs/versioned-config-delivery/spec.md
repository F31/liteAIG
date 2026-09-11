# versioned-config-delivery Specification (Delta)

## ADDED Requirements

### Requirement: Signed bundle delivery with prepare and LKG
Published configuration SHALL be delivered to the Data Plane as a signed RuntimeBundle with Prepare/ACK/NACK activation and local Last Known Good persistence, as specified by the runtime-bundle-signing, prepare-ack-nack-activation, and last-known-good-boot capabilities. A NACKed bundle SHALL NOT replace the active version, and boot SHALL fall back to a signature-valid LKG when the Control Plane is unreachable.

#### Scenario: Publication activates via signed bundle
- **WHEN** an operator publishes a config version
- **THEN** it is delivered as a signed RuntimeBundle
- **AND** the Data Plane activates it only after Prepare verification
