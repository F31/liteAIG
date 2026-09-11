package adminapi

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/F31/liteAIG/internal/platform/webkit"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	identityoidc "github.com/F31/liteAIG/internal/identity/oidc"
)

type testOIDCExchange struct{ verifier string }

func (e *testOIDCExchange) Exchange(_ context.Context, code, verifier, redirectURI string) (string, error) {
	if code != "authorization-code" || redirectURI != "https://console.example/login" {
		return "", context.Canceled
	}
	e.verifier = verifier
	return "verified-id-token", nil
}

func TestOIDCPKCELoginCreatesSession(t *testing.T) {
	manager, err := NewSessionManager(SessionConfig{CookieName: "lia", TTL: time.Hour, Secure: true}, &sessionClock{now: time.Unix(100, 0)}, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	exchange := &testOIDCExchange{}
	login, err := NewOIDCLogin(manager, &identityoidc.MockVerifier{Valid: &identityoidc.Claims{Sub: "user-1", Aud: "console"}}, exchange, OIDCLoginConfig{
		AuthorizationEndpoint: "https://idp.example/authorize", ClientID: "client", Audience: "console",
		RedirectURI: "https://console.example/login", Roles: identityoidc.RoleClaims{Admins: []string{"user-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	login.random = bytes.NewReader(bytes.Repeat([]byte{1}, 64))
	login.now = func() time.Time { return time.Unix(100, 0) }
	mux := webkit.New()
	SessionEndpoints{Sessions: manager, Verifier: &verifier{}, OIDC: login, MaxBodyBytes: 1024}.Register(mux)

	start := httptest.NewRecorder()
	mux.ServeHTTP(start, httptest.NewRequest(http.MethodPost, "/api/admin/oidc/start", nil))
	if start.Code != http.StatusOK {
		t.Fatalf("start = %d %s", start.Code, start.Body.String())
	}
	var started struct {
		AuthorizationURL string `json:"authorizationUrl"`
	}
	if err := json.NewDecoder(start.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	authorizationURL, err := url.Parse(started.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	query := authorizationURL.Query()
	if query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" || query.Get("state") == "" {
		t.Fatalf("PKCE query = %v", query)
	}

	body := strings.NewReader(`{"code":"authorization-code","state":"` + query.Get("state") + `"}`)
	callback := httptest.NewRecorder()
	mux.ServeHTTP(callback, httptest.NewRequest(http.MethodPost, "/api/admin/oidc/session", body))
	if callback.Code != http.StatusOK || exchange.verifier == "" {
		t.Fatalf("session = %d %s", callback.Code, callback.Body.String())
	}
	cookies := callback.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatalf("session cookie = %+v", cookies)
	}

	replay := httptest.NewRecorder()
	mux.ServeHTTP(replay, httptest.NewRequest(http.MethodPost, "/api/admin/oidc/session", strings.NewReader(`{"code":"authorization-code","state":"`+query.Get("state")+`"}`)))
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("state replay = %d", replay.Code)
	}
}

func TestOIDCSessionResolvesRealTenant(t *testing.T) {
	manager, err := NewSessionManager(SessionConfig{CookieName: "lia", TTL: time.Hour, Secure: true}, &sessionClock{now: time.Unix(100, 0)}, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	login, err := NewOIDCLogin(manager, &identityoidc.MockVerifier{Valid: &identityoidc.Claims{Sub: "user-1", Aud: "console"}}, &testOIDCExchange{}, OIDCLoginConfig{
		AuthorizationEndpoint: "https://idp.example/authorize", ClientID: "client", Audience: "console",
		RedirectURI: "https://console.example/login", Roles: identityoidc.RoleClaims{Admins: []string{"user-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	login.random = bytes.NewReader(bytes.Repeat([]byte{1}, 64))
	login.now = func() time.Time { return time.Unix(100, 0) }
	// Single-tenant Lite: the session must point at the real bootstrap tenant,
	// not the "default" placeholder the claim mapping produces.
	login.ResolveTenant = func(context.Context) (string, error) { return "tenant-42", nil }
	mux := webkit.New()
	SessionEndpoints{Sessions: manager, Verifier: &verifier{}, OIDC: login, MaxBodyBytes: 1024}.Register(mux)

	start := httptest.NewRecorder()
	mux.ServeHTTP(start, httptest.NewRequest(http.MethodPost, "/api/admin/oidc/start", nil))
	var started struct {
		AuthorizationURL string `json:"authorizationUrl"`
	}
	_ = json.NewDecoder(start.Body).Decode(&started)
	query, _ := url.Parse(started.AuthorizationURL)

	body := strings.NewReader(`{"code":"authorization-code","state":"` + query.Query().Get("state") + `"}`)
	callback := httptest.NewRecorder()
	mux.ServeHTTP(callback, httptest.NewRequest(http.MethodPost, "/api/admin/oidc/session", body))
	if callback.Code != http.StatusOK {
		t.Fatalf("session = %d %s", callback.Code, callback.Body.String())
	}

	// Replay the cookie through the authorizer: the session's tenant must be
	// the resolved tenant.
	request := httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil)
	for _, cookie := range callback.Result().Cookies() {
		request.AddCookie(cookie)
	}
	session, err := manager.Authorize(request)
	if err != nil {
		t.Fatal(err)
	}
	if session.TenantID != "tenant-42" {
		t.Fatalf("session tenant = %q, want tenant-42", session.TenantID)
	}
}

func TestHTTPTokenExchangerUsesPKCE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("code_verifier") != "verifier" || r.Form.Get("client_secret") != "" {
			t.Fatalf("token form = %v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": "token"})
	}))
	defer server.Close()
	exchanger := HTTPTokenExchanger{Client: server.Client(), TokenEndpoint: server.URL, ClientID: "client"}
	token, err := exchanger.Exchange(context.Background(), "code", "verifier", "https://console.example/login")
	if err != nil || token != "token" {
		t.Fatalf("exchange = %q, %v", token, err)
	}
}
