package vertextoken

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testKeyPEM is a generated RSA key used only in tests.
func generateKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

func TestParseServiceAccount(t *testing.T) {
	privateKey := generateKeyPEM(t)
	account, err := ParseServiceAccount([]byte(`{"client_email":"a@example.com","private_key":"` + strings.ReplaceAll(privateKey, "\n", "\\n") + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	if account.ClientEmail != "a@example.com" {
		t.Fatalf("client email = %q", account.ClientEmail)
	}
	if account.TokenURI != "https://oauth2.googleapis.com/token" {
		t.Fatalf("default token URI = %q", account.TokenURI)
	}
}

func TestParseMalformedServiceAccount(t *testing.T) {
	if _, err := ParseServiceAccount([]byte("not-json")); err == nil {
		t.Fatal("malformed service account must error")
	}
}

func TestParsePrivateKey(t *testing.T) {
	privateKey := generateKeyPEM(t)
	if _, err := ParsePrivateKey([]byte(privateKey)); err != nil {
		t.Fatal(err)
	}
}

func TestAccessTokenExchangeAndCache(t *testing.T) {
	var exchanges int
	var gotGrant string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanges++
		_ = r.ParseForm()
		gotGrant = r.Form.Get("grant_type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"ya29.tokenvalue","expires_in":3600}`))
	}))
	defer server.Close()

	privateKey := generateKeyPEM(t)
	key, err := ParsePrivateKey([]byte(privateKey))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0)
	signer := New(ServiceAccount{ClientEmail: "a@example.com", PrivateKey: privateKey, TokenURI: server.URL, ProjectID: "proj"}, key, server.Client(), func() time.Time { return now })

	token, err := signer.AccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token != "ya29.tokenvalue" {
		t.Fatalf("token = %q", token)
	}
	if gotGrant != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
		t.Fatalf("grant_type = %q", gotGrant)
	}
	// A second call within the token lifetime must hit the cache.
	if _, err := signer.AccessToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if exchanges != 1 {
		t.Fatalf("exchanges = %d, want 1 (cached)", exchanges)
	}
	// Advancing past expiry mints a fresh token.
	now = now.Add(2 * time.Hour)
	if _, err := signer.AccessToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if exchanges != 2 {
		t.Fatalf("exchanges after expiry = %d, want 2", exchanges)
	}
}

func TestAssertionIsThreePartJWT(t *testing.T) {
	privateKey := generateKeyPEM(t)
	key, err := ParsePrivateKey([]byte(privateKey))
	if err != nil {
		t.Fatal(err)
	}
	signer := New(ServiceAccount{ClientEmail: "a@example.com", PrivateKey: privateKey, TokenURI: "https://oauth2.googleapis.com/token", ProjectID: "proj"}, key, &http.Client{}, func() time.Time { return time.Unix(1000, 0) })
	assertion, err := signer.assertion()
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(assertion, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		t.Fatalf("assertion parts = %d", len(parts))
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	var header map[string]string
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatal(err)
	}
	if header["alg"] != "RS256" || header["typ"] != "JWT" {
		t.Fatalf("jwt header = %+v", header)
	}
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		Iss   string `json:"iss"`
		Scope string `json:"scope"`
		Aud   string `json:"aud"`
		Iat   int64  `json:"iat"`
		Exp   int64  `json:"exp"`
	}
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Iss != "a@example.com" || claims.Scope == "" || claims.Aud == "" || claims.Exp-claims.Iat != 3600 {
		t.Fatalf("jwt claims = %+v", claims)
	}
}
