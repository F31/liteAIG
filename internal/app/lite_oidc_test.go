package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newJWKSServer(t *testing.T) *httptest.Server {
	jwks := map[string]any{"keys": []map[string]any{{"kty": "RSA", "kid": "k1", "alg": "RS256", "n": "test", "e": "AQAB"}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(server.Close)
	return server
}

// TestOIDCDisabledByDefault proves the SSO surface is hidden when the
// composition root gets no OIDC options, and enabled once they are provided.
func TestOIDCDisabledByDefault(t *testing.T) {
	lite, err := NewLite(context.Background(), LiteOptions{DSN: testMemoryDSN(t, "oidc-off")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lite.Close() })
	admin := httptest.NewServer(lite.Handler())
	t.Cleanup(admin.Close)

	status, body := doJSON(t, admin.URL+"/api/admin/oidc/config", "GET", nil, "", "")
	var config struct {
		Enabled bool `json:"enabled"`
	}
	if status != http.StatusOK || json.Unmarshal([]byte(body), &config) != nil || config.Enabled {
		t.Fatalf("oidc config status=%d body=%s, want enabled=false", status, body)
	}
}

func TestOIDCEndpointsEnabledWhenConfigured(t *testing.T) {
	jwks := newJWKSServer(t)
	token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer token.Close()

	lite, err := NewLite(context.Background(), LiteOptions{
		DSN: testMemoryDSN(t, "oidc-on"),
		OIDC: &OIDCOptions{
			Issuer:                jwks.URL,
			ClientID:              "liteaig-console",
			AuthorizationEndpoint: jwks.URL + "/authorize",
			TokenEndpoint:         token.URL,
			RedirectURI:           "http://127.0.0.1:18081/login",
			StateTTL:              time.Minute,
			AdminSubjects:         []string{"admin-1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lite.Close() })
	admin := httptest.NewServer(lite.Handler())
	t.Cleanup(admin.Close)

	status, body := doJSON(t, admin.URL+"/api/admin/oidc/config", "GET", nil, "", "")
	var config struct {
		Enabled bool `json:"enabled"`
	}
	if status != http.StatusOK || json.Unmarshal([]byte(body), &config) != nil || !config.Enabled {
		t.Fatalf("oidc config status=%d body=%s, want enabled=true", status, body)
	}

	status, body = doJSON(t, admin.URL+"/api/admin/oidc/start", "POST", map[string]string{}, "", "")
	if status != http.StatusOK {
		t.Fatalf("oidc start status=%d body=%s", status, body)
	}
	var start struct {
		AuthorizationURL string `json:"authorizationUrl"`
	}
	if err := json.Unmarshal([]byte(body), &start); err != nil {
		t.Fatal(err)
	}
	if start.AuthorizationURL == "" {
		t.Fatalf("oidc start = %s, want an authorization url", body)
	}
	for _, want := range []string{"client_id=liteaig-console", "code_challenge=", "redirect_uri="} {
		if !strings.Contains(start.AuthorizationURL, want) {
			t.Fatalf("authorization url %s missing %q", start.AuthorizationURL, want)
		}
	}
}

func TestOIDCPartialConfigurationFailsStartup(t *testing.T) {
	if _, err := NewLite(context.Background(), LiteOptions{
		DSN:  testMemoryDSN(t, "oidc-bad"),
		OIDC: &OIDCOptions{Issuer: "http://127.0.0.1:1", ClientID: "client"},
	}); err == nil {
		t.Fatal("startup succeeded with an incomplete OIDC configuration")
	}
}
