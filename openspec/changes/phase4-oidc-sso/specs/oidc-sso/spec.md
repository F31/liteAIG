# oidc-sso Specification

## ADDED Requirements

### Requirement: OIDC ID-token verification
The system SHALL verify OIDC ID tokens from a configured issuer: discover the provider configuration and JWKS, verify the token signature, and validate `iss`, `aud`, `exp`, `iat`, and `jti` (with a short replay window). A deterministic mock verifier SHALL be available for tests.

#### Scenario: Expired token rejected
- **WHEN** an ID token is expired
- **THEN** verification fails
- **AND** no principal is established

#### Scenario: Wrong audience rejected
- **WHEN** an ID token's audience does not match the configured audience
- **THEN** verification fails
- **AND** no session is created

#### Scenario: Replay token rejected
- **WHEN** a `jti` has been seen within the replay window
- **THEN** verification fails as a replay

### Requirement: Backend-authoritative user mapping
Verified ID-token claims SHALL map to a Principal and role using only the verified token's configured claims. Client-supplied roles SHALL NOT be trusted.

#### Scenario: Role from verified token
- **WHEN** a verified ID token carries the configured role claim
- **THEN** the user's tenant role is derived from it
- **AND** a client-supplied role is ignored
