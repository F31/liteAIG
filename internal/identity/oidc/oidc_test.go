package oidc

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMockVerifierSemantics(t *testing.T) {
	ctx := context.Background()
	valid := &Claims{Iss: "iss", Sub: "u1", Aud: "aud", Exp: time.Now().Add(time.Hour).Unix(), Iat: time.Now().Unix(), Jti: "j1"}
	verifier := &MockVerifier{Valid: valid}
	claims, err := verifier.VerifyIDToken(ctx, "token", "aud")
	if err != nil || claims.Sub != "u1" {
		t.Fatalf("verify = %+v, %v", claims, err)
	}
	if _, err := verifier.VerifyIDToken(ctx, "token", "other-aud"); err != ErrAudience {
		t.Fatalf("audience error = %v", err)
	}
	failing := &MockVerifier{Err: ErrExpired}
	if _, err := failing.VerifyIDToken(ctx, "token", "aud"); err != ErrExpired {
		t.Fatalf("error = %v", err)
	}
}

func TestJWKSVerifierReplayWindow(t *testing.T) {
	verifier := NewJWKSVerifier(nil, "https://issuer", time.Minute)
	now := time.Unix(1000, 0)
	verifier.now = func() time.Time { return now }
	claims := &Claims{Iss: "https://issuer", Sub: "u1", Aud: "aud", Exp: now.Add(time.Hour).Unix(), Iat: now.Unix(), Jti: "replay-1"}
	verifier.acceptJTI(claims.Jti, now)
	if verifier.acceptJTI("replay-1", now.Add(30*time.Second)) {
		t.Fatal("replayed jti accepted within window")
	}
	if !verifier.acceptJTI("replay-1", now.Add(2*time.Minute)) {
		t.Fatal("jti outside window rejected")
	}
}

func signJWT(t *testing.T, key *rsa.PrivateKey, header map[string]any, claims map[string]any) string {
	t.Helper()
	headerBytes, _ := json.Marshal(header)
	claimsBytes, _ := json.Marshal(claims)
	headerPart := base64.RawURLEncoding.EncodeToString(headerBytes)
	claimsPart := base64.RawURLEncoding.EncodeToString(claimsBytes)
	message := headerPart + "." + claimsPart
	digest := sha256.Sum256([]byte(message))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return message + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func TestJWKSVerifierValidatesRealSignature(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := x509.ParsePKIXPublicKey(pubDER)
	if err != nil {
		t.Fatal(err)
	}
	rsaPub := pub.(*rsa.PublicKey)

	// Serve a JWKS exposing the modulus/exponent.
	jwks := map[string]any{"keys": []map[string]any{{"kty": "RSA", "kid": "k1", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(rsaPub.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(rsaPub.E)).Bytes())}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	defer server.Close()

	verifier := NewJWKSVerifier(server.Client(), server.URL, time.Minute)
	now := time.Now()
	token := signJWT(t, key, map[string]any{"alg": "RS256", "kid": "k1"}, map[string]any{"iss": server.URL, "sub": "u1", "aud": "aud", "exp": now.Add(time.Hour).Unix(), "iat": now.Unix(), "jti": "j1", "email": "a@example.com"})
	claims, err := verifier.VerifyIDToken(context.Background(), token, "aud")
	if err != nil || claims.Sub != "u1" {
		t.Fatalf("verify = %+v, %v", claims, err)
	}

	// Tampered token → signature failure.
	tampered := token[:len(token)-1] + "x"
	if _, err := verifier.VerifyIDToken(context.Background(), tampered, "aud"); err == nil {
		t.Fatal("tampered token verified")
	}

	// Expired token → rejected.
	expired := signJWT(t, key, map[string]any{"alg": "RS256", "kid": "k1"}, map[string]any{"iss": server.URL, "sub": "u1", "aud": "aud", "exp": now.Add(-time.Hour).Unix(), "iat": now.Add(-2 * time.Hour).Unix(), "jti": "j2"})
	if _, err := verifier.VerifyIDToken(context.Background(), expired, "aud"); err != ErrExpired {
		t.Fatalf("expired error = %v", err)
	}

	// Wrong audience → rejected.
	wrongAud := signJWT(t, key, map[string]any{"alg": "RS256", "kid": "k1"}, map[string]any{"iss": server.URL, "sub": "u1", "aud": "other", "exp": now.Add(time.Hour).Unix(), "iat": now.Unix(), "jti": "j3"})
	if _, err := verifier.VerifyIDToken(context.Background(), wrongAud, "aud"); err != ErrAudience {
		t.Fatalf("audience error = %v", err)
	}
}

func TestMapToPrincipalUsesVerifiedClaimsOnly(t *testing.T) {
	claims := &Claims{Sub: "u-admin", Email: "admin@example.com"}
	roles := RoleClaims{Admins: []string{"u-admin"}, Operators: []string{"u-ops"}}
	principal := MapToPrincipal(claims, roles)
	if principal.Role != "tenant_admin" || principal.ExternalSubject != "u-admin" || principal.AttributionTrust != "verified" {
		t.Fatalf("principal = %+v", principal)
	}
	// A non-listed subject maps to viewer regardless of any client role.
	viewer := MapToPrincipal(&Claims{Sub: "u-other"}, roles)
	if viewer.Role != "viewer" {
		t.Fatalf("viewer role = %s", viewer.Role)
	}
	operator := MapToPrincipal(&Claims{Sub: "u-ops"}, roles)
	if operator.Role != "tenant_operator" {
		t.Fatalf("operator role = %s", operator.Role)
	}
}
