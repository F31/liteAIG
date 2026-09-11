// Package oidc provides backend-authoritative OIDC ID-token verification
// (discovery, JWKS, signature, iss/aud/exp/iat/jti) and a deterministic mock for
// tests. Standard-library only.
package oidc

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/F31/liteAIG/internal/identity"
)

// Claims are the verified ID-token claims.
type Claims struct {
	Iss               string `json:"iss"`
	Sub               string `json:"sub"`
	Aud               string `json:"aud"`
	Exp               int64  `json:"exp"`
	Iat               int64  `json:"iat"`
	Jti               string `json:"jti"`
	Email             string `json:"email"`
	PreferredUsername string `json:"preferred_username"`
}

// Verifier verifies an OIDC ID token.
type Verifier interface {
	VerifyIDToken(context.Context, string, string) (*Claims, error)
}

var (
	ErrExpired     = errors.New("id token expired")
	ErrNotYetValid = errors.New("id token not yet valid")
	ErrAudience    = errors.New("id token audience mismatch")
	ErrIssuer      = errors.New("id token issuer mismatch")
	ErrSignature   = errors.New("id token signature invalid")
	ErrReplay      = errors.New("id token replay")
)

// jwksKey is one JWKS signing key.
type jwksKey struct {
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
	Alg string `json:"alg"`
}

// jwksDocument is the JWKS discovery payload.
type jwksDocument struct {
	Keys []jwksKey `json:"keys"`
}

// JWKSVerifier verifies tokens using a fetched JWKS key set.
type JWKSVerifier struct {
	client       *http.Client
	issuer       string
	replayWindow time.Duration
	mu           sync.Mutex
	usedJTIs     map[string]time.Time
	now          func() time.Time
}

// NewJWKSVerifier builds a verifier for an issuer with a replay window.
func NewJWKSVerifier(client *http.Client, issuer string, replayWindow time.Duration) *JWKSVerifier {
	if client == nil {
		client = http.DefaultClient
	}
	if replayWindow <= 0 {
		replayWindow = time.Minute
	}
	return &JWKSVerifier{client: client, issuer: issuer, replayWindow: replayWindow, usedJTIs: map[string]time.Time{}, now: time.Now}
}

// VerifyIDToken validates the token against the issuer's JWKS.
func (v *JWKSVerifier) VerifyIDToken(ctx context.Context, token, expectedAudience string) (*Claims, error) {
	_, payload, err := decodeJWT(token)
	if err != nil {
		return nil, err
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	if claims.Iss != v.issuer {
		return nil, ErrIssuer
	}
	if claims.Aud != expectedAudience {
		return nil, ErrAudience
	}
	now := v.now()
	if now.After(time.Unix(claims.Exp, 0)) {
		return nil, ErrExpired
	}
	if now.Before(time.Unix(claims.Iat, 0)) {
		return nil, ErrNotYetValid
	}
	if err := v.verifySignature(ctx, token); err != nil {
		return nil, err
	}
	if claims.Jti != "" && !v.acceptJTI(claims.Jti, now) {
		return nil, ErrReplay
	}
	return &claims, nil
}

func (v *JWKSVerifier) acceptJTI(jti string, now time.Time) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if seenAt, ok := v.usedJTIs[jti]; ok && now.Sub(seenAt) <= v.replayWindow {
		return false
	}
	v.usedJTIs[jti] = now
	return true
}

func (v *JWKSVerifier) verifySignature(ctx context.Context, token string) error {
	keys, err := v.loadKeys(ctx)
	if err != nil {
		return err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts(token)[2])
	if err != nil {
		return err
	}
	message := parts(token)[0] + "." + parts(token)[1]
	for _, key := range keys.Keys {
		nBytes, err1 := base64.RawURLEncoding.DecodeString(key.N)
		if err1 != nil {
			continue
		}
		eBytes, err1 := base64.RawURLEncoding.DecodeString(key.E)
		if err1 != nil {
			continue
		}
		pub := &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: int(new(big.Int).SetBytes(eBytes).Int64())}
		digest := sha256.Sum256([]byte(message))
		if rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], signature) == nil {
			return nil
		}
	}
	return ErrSignature
}

func (v *JWKSVerifier) loadKeys(ctx context.Context) (jwksDocument, error) {
	var document jwksDocument
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, v.issuer+"/.well-known/jwks.json", nil)
	if err != nil {
		return document, err
	}
	response, err := v.client.Do(request)
	if err != nil {
		return document, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return document, errors.New("jwks fetch failed")
	}
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		return document, err
	}
	return document, nil
}

func parts(token string) []string { return strings.Split(token, ".") }

func decodeJWT(token string) (map[string]any, []byte, error) {
	part := parts(token)
	if len(part) != 3 {
		return nil, nil, errors.New("malformed jwt")
	}
	var header map[string]any
	headerBytes, err := base64.RawURLEncoding.DecodeString(part[0])
	if err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, nil, err
	}
	payload, err := base64.RawURLEncoding.DecodeString(part[1])
	if err != nil {
		return nil, nil, err
	}
	return header, payload, nil
}

// MockVerifier is a deterministic verifier for tests.
type MockVerifier struct {
	ExpectedAudience string
	Valid            *Claims
	Err              error
	now              func() time.Time
}

// VerifyIDToken returns the configured result.
func (m *MockVerifier) VerifyIDToken(_ context.Context, _, audience string) (*Claims, error) {
	if m.Valid != nil && m.Valid.Aud != audience {
		return nil, ErrAudience
	}
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Valid, nil
}

// RoleClaims maps verified claims to a tenant role (backend-authoritative).
type RoleClaims struct {
	RoleClaim string   // JWT claim name carrying the role
	Admins    []string // allowed admin subjects
	Operators []string // allowed operator subjects
}

// MapToPrincipal derives a Principal from verified claims. The role comes only
// from the verified token's configured claim; a client-supplied role is ignored.
func MapToPrincipal(claims *Claims, roles RoleClaims) Principal {
	principal := Principal{
		Type:             identity.PrincipalUser,
		TenantID:         "default",
		AuthMethod:       identity.AuthMethodOIDC,
		ExternalSubject:  claims.Sub,
		AttributionTrust: identity.AttributionVerified,
	}
	for _, subject := range roles.Admins {
		if subject == claims.Sub {
			principal.Role = "tenant_admin"
			return principal
		}
	}
	for _, subject := range roles.Operators {
		if subject == claims.Sub {
			principal.Role = "tenant_operator"
			return principal
		}
	}
	principal.Role = "viewer"
	return principal
}

// Principal is the minimal mapped identity. (The full canonical Principal is in
// the identity package; this slice maps the role + subject for the sign-in path.)
type Principal struct {
	Type             string
	TenantID         string
	Role             string
	AuthMethod       string
	ExternalSubject  string
	AttributionTrust string
}
