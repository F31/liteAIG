# Phase 4 — OIDC SSO Integration: Design

## Context

Console/Admin sign-in is local-only (`internal/controlplane/adminapi/localauth.go`),
using the session cookie + CSRF path. There is no external identity provider.
This change adds OIDC SSO as a backend-authoritative path: discover the issuer,
load JWKS keys, validate ID tokens, and map trusted claims to a Principal and
role. Local auth remains the fallback.

## Goals / Non-Goals

**Goals:**
- OIDC discovery + JWKS loading + ID-token validation (iss/aud/exp/iat/jti).
- User claim → Principal/role mapping, backend-authoritative.
- Console OIDC sign-in (authorization code + PKCE + token exchange) behind a
  replaceable `TokenVerifier`.
- SCIM provisioning contract + deterministic mock.

**Non-Goals:**
- SAML, full SCIM API server, MFA/IdP-side flows, ClickHouse sink, Multi-AZ/DR,
  gRPC Extension Bridge.

## Decisions

### 1. OIDC verification is a replaceable contract
New `internal/identity/oidc`: `Verifier` interface with `VerifyIDToken(ctx,
idToken, expectedAudience) (*Claims, error)`. The `JWKSVerifier` performs OIDC
discovery (well-known config → jwks_uri), loads signing keys, verifies the JWT
signature (RSA via stdlib), and validates `iss`, `aud`, `exp`, `iat`, `jti`
(uniqueness within a short replay window). A `MockVerifier` is deterministic for
tests.
Alternative considered: an OIDC SDK. Rejected — stdlib-only keeps the no-new-
dependency rule and the verification is self-contained.

### 2. Mapping is backend-authoritative
`Claims → identity.Principal`: `sub` → Agent/User subject, `email/preferred_username`
→ display name, and a configurable claim/role map → tenant role (admin/operator/
viewer). No client-supplied role is trusted.

### 3. Console sign-in flows through a token exchange
The admin API gains an OIDC sign-in endpoint: it exchanges the authorization
code (+ PKCE) for tokens, verifies the ID token, maps the user, and issues the
same Secure/HttpOnly session cookie. Local auth is retained when OIDC is not
configured.

### 4. SCIM is a provisioning contract
New `internal/identity/scim`: `Provisioner` with `UpsertUser(ctx, user)` and a
`MockProvisioner`. A full SCIM REST server is a follow-up.

## Risks / Trade-offs

- [JWKS fetch failure] → cache keys with TTL; fail closed on signature errors.
- [Replay] → jti replay window + exp/iat bounds.
- [Role spoofing] → roles come from the verified token's configured claim, never the client.

## Migration Plan

1. Add `internal/identity/oidc` (discovery, JWKS, verifier, mapper) with tests.
2. Add `internal/identity/scim` (contract + mock) with tests.
3. Wire the admin API OIDC sign-in endpoint (PKCE + exchange + session) behind
   the verifier; local auth remains the default.
4. Add the localized Console OIDC surface.
5. Run full gate set + OpenSpec strict validation + record DoD evidence.

Rollback: OIDC disabled when no issuer is configured; local auth stays the
default path.
