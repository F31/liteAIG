## 1. OIDC Verification

- [x] 1.1 Add `internal/identity/oidc`: `Verifier` contract (`VerifyIDToken`), `Claims`, and a `JWKSVerifier` (discovery + JWKS + signature via stdlib RSA).
- [x] 1.2 Validate `iss`, `aud`, `exp`, `iat`, and `jti` replay window.
- [x] 1.3 Add a deterministic `MockVerifier` and self-signed JWT test helpers.
- [x] 1.4 Add tests: expired rejected, wrong audience rejected, bad signature rejected, replay jti rejected, valid token verified.

## 2. User Mapping and SCIM

- [x] 2.1 Add claim → `identity.Principal`/role mapping (backend-authoritative; client roles ignored).
- [x] 2.2 Add `internal/identity/scim`: `Provisioner` contract (`UpsertUser`, `DeactivateUser`) + `MockProvisioner`.
- [x] 2.3 Add tests: mapping from verified claims, client-role ignored, mock provisioning deterministic.

## 3. Console Sign-In and Release Gates

- [x] 3.1 Wire an OIDC sign-in endpoint in the admin API (PKCE + token exchange + verified ID token → session cookie); local auth stays the default.
- [x] 3.2 Add a localized Console OIDC surface (login button + error states).
- [x] 3.3 Run the full gate set (`go test -race ./...`, `go vet ./...`, architecture-test, `npm run check`, actionlint, gitleaks) and resolve blocking failures.
- [x] 3.4 Run `openspec validate --all --strict` and record Phase 4 OIDC/SCIM DoD evidence.
