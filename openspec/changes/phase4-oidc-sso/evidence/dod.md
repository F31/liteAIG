# Phase 4 — OIDC SSO Integration: DoD Evidence

Status: Complete (11/11 tasks)

This change delivers the first, testable slice of the Phase 4 identity
integration: backend-authoritative OIDC ID-token verification, user→Principal
mapping, a PKCE Console sign-in path, and a SCIM provisioning contract.

## Capabilities Delivered

### OIDC SSO (`oidc-sso`)
- `internal/identity/oidc`: `Verifier` contract, `Claims`, and a
  `JWKSVerifier` (discovery + JWKS loading + RSA signature verification via
  stdlib), validating `iss`, `aud`, `exp`, `iat`, and `jti` replay window.
- Deterministic `MockVerifier` for tests.
- `MapToPrincipal`: verified claims → Principal/role, backend-authoritative
  (a client-supplied role is ignored).
- `internal/controlplane/adminapi`: short-lived, single-use OIDC state; S256
  PKCE authorization; backend token exchange; verified ID token to the existing
  Secure/HttpOnly session cookie. Local login remains available and is the
  default when OIDC is not configured.
- `web/console/src/pages/Login.tsx`: localized local/OIDC login and non-sensitive
  callback error states; ID/access tokens never enter browser storage.

### SCIM User Provisioning (`scim-user-provisioning`)
- `internal/identity/scim`: `Provisioner` contract (`UpsertUser`,
  `DeactivateUser`) + `MockProvisioner`.

## Gate Results

| Gate | Result |
| --- | --- |
| `go test -race -timeout 180s ./...` (Postgres 55432 + Redis 56379) | PASS |
| `go vet ./...` | PASS |
| `go run ./cmd/architecture-test` | PASS |
| `actionlint` | PASS |
| `gitleaks --config .gitleaks.toml detect` | no leaks found |
| `npm run check` | PASS (7 tests, locale/string checks, production build) |
| `openspec validate --all --strict` | 24 passed, 0 failed |

## Tests

- OIDC: expired rejected, wrong audience rejected, real RSA JWKS signature
  verified, tampered token rejected, replay `jti` rejected within window.
- Mapping: verified-claim role derivation (admin/operator/viewer); non-listed
  subject maps to viewer regardless of client role.
- SCIM: upsert updates, deactivate marks inactive, missing user returns
  ErrNotFound, calls are deterministic.
- OIDC login: S256 challenge generated, state consumed once, token exchange
  carries the verifier without a client secret, and the session cookie is
  Secure/HttpOnly. Console local and SSO choices are localized.

## Scope Note

SAML, a full SCIM REST server, and MFA/IdP-side flows remain out of scope.
