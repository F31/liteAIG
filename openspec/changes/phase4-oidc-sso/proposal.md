# Phase 4 — OIDC SSO Integration

## Why

V8.2 Phase 4 lists OIDC/SAML/SCIM as identity integration. Today Console/Admin authentication is local-only (`internal/controlplane/adminapi/localauth.go`). This change adds OIDC SSO as a backend-authoritative sign-in path: OIDC discovery, JWKS key loading, ID-token validation (iss/aud/sub/exp/iat/jti), and user→Principal mapping. It is the first, testable slice of the identity integration; SAML and SCIM remain separate remainder items.

## What Changes

- Add an OIDC integration: discover the provider configuration and JWKS from the issuer, load signing keys, validate ID tokens (signature, `iss`, `aud`, `exp`, `iat`, `jti` uniqueness window), and map trusted user claims to a Principal/role.
- Add a validation contract with a deterministic mock IdP for tests (stdlib RSA keys + self-signed JWT).
- Add a backend-authoritative OIDC sign-in path for the Console (authorization code + PKCE + token exchange + user mapping) behind a replaceable `TokenVerifier`, with local auth retained as the fallback.
- Add the SCIM sync contract (user provisioning) as an interface with a deterministic mock, scoped to this slice.

## Capabilities

### New Capabilities
- `oidc-sso`: OIDC discovery, JWKS verification, ID-token validation, and user→Principal mapping for Console sign-in.
- `scim-user-provisioning`: SCIM user/group provisioning contract with a deterministic mock.

### Modified Capabilities
- `multimodel-protocol-access`: Console/Admin authentication gains an OIDC path alongside local auth.

## Impact

- **Backend**: new `internal/identity/oidc` (discovery, JWKS, verifier, mapper) and `internal/identity/scim` (contract + mock); admin API sign-in wiring; config for issuer/audience/roles.
- **APIs/Console**: localized OIDC sign-in surface (login button + error states); admin login retains local fallback.
- **Dependencies**: standard-library only (crypto/rsa, encoding/json, net/http for JWKS).
- **Tests**: ID-token validation (expired, wrong audience, bad signature, replay jti rejected), discovery/JWKS parse, user claim mapping, mock SCIM provisioning, local-auth fallback preserved.

### Non-Goals
- SAML, full SCIM API server, MFA/IdP-side flows, ClickHouse sink, Multi-AZ/DR orchestration (separate remainder items).

**Golden Scenario:** strengthens Scenario G (identity continuity) and the Console journey (SSO sign-in).
