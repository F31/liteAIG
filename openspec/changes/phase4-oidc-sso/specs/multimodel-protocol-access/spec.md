# multimodel-protocol-access Specification (Delta)

## ADDED Requirements

### Requirement: OIDC sign-in alongside local auth
Console/Admin authentication SHALL support an OIDC SSO path (authorization code + PKCE + token exchange + ID-token verification) alongside the existing local-auth path. Local auth SHALL remain the default when no OIDC issuer is configured, and the OIDC path SHALL issue the same Secure/HttpOnly session cookie.

#### Scenario: OIDC sign-in establishes a session
- **WHEN** a user completes the OIDC flow with a verified ID token
- **THEN** a Secure/HttpOnly session cookie is issued
- **AND** local auth remains available as a fallback
