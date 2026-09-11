package adminapi

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/F31/liteAIG/internal/platform/webkit"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type verifier struct{ password []byte }

func (v *verifier) Verify(_ context.Context, _ string, password []byte) (Session, error) {
	v.password = append([]byte(nil), password...)
	return Session{AdminID: "admin", TenantID: "tenant"}, nil
}
func TestLoginReturnsCSRFAndSecureCookieWithoutPassword(t *testing.T) {
	manager, _ := NewSessionManager(SessionConfig{CookieName: "lia", TTL: time.Hour, Secure: true}, &sessionClock{now: time.Unix(1, 0)}, bytes.NewReader(make([]byte, 64)))
	verifier := &verifier{}
	mux := webkit.New()
	SessionEndpoints{Sessions: manager, Verifier: verifier, MaxBodyBytes: 1024}.Register(mux)
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "temporary-password"})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/admin/session", bytes.NewReader(body)))
	if response.Code != 200 || bytes.Contains(response.Body.Bytes(), []byte("temporary-password")) {
		t.Fatalf("response=%d %s", response.Code, response.Body.String())
	}
	if string(verifier.password) != "temporary-password" || len(response.Result().Cookies()) != 1 {
		t.Fatalf("verification/cookie failed")
	}
}

type resetStore struct{ username, hash string }

func (s *resetStore) FindLocalCredential(_ context.Context, _ string) (LocalCredential, error) {
	return LocalCredential{AdminID: "admin", TenantID: "tenant", Username: "admin", PasswordHash: s.hash, Status: "active"}, nil
}
func (s *resetStore) FindLocalUserByID(_ context.Context, id string) (LocalCredential, error) {
	if id != "admin" {
		return LocalCredential{}, ErrLocalUserNotFound
	}
	return LocalCredential{AdminID: "admin", TenantID: "tenant", Username: "admin", PasswordHash: s.hash, Status: "active"}, nil
}
func (s *resetStore) ResetLocalPassword(_ context.Context, username, hash string) error {
	s.username, s.hash = username, hash
	return nil
}
func (s *resetStore) ListLocalUsers(context.Context) ([]LocalUser, error) { return nil, nil }
func (s *resetStore) CreateLocalUser(context.Context, LocalUser) (LocalUser, error) {
	return LocalUser{}, ErrUsernameTaken
}
func (s *resetStore) SetUserRole(context.Context, string, string) (LocalUser, error) {
	return LocalUser{}, ErrLocalUserNotFound
}
func (s *resetStore) SetUserStatus(context.Context, string, string) (LocalUser, error) {
	return LocalUser{}, ErrLocalUserNotFound
}
func (s *resetStore) SetUserEmail(context.Context, string, string) (LocalUser, error) {
	return LocalUser{}, ErrLocalUserNotFound
}
func (s *resetStore) DeleteLocalUser(context.Context, string) error  { return ErrLocalUserNotFound }
func (s *resetStore) CountActiveAdmins(context.Context) (int, error) { return 1, nil }

type resetPasswords struct{}

func (resetPasswords) Hash(password []byte) (string, error) { return "hash:" + string(password), nil }
func (resetPasswords) Verify([]byte, string) (bool, error)  { return true, nil }

func TestResetPasswordRequiresLoopback(t *testing.T) {
	manager, _ := NewSessionManager(SessionConfig{CookieName: "lia", TTL: time.Hour, Secure: true}, &sessionClock{now: time.Unix(1, 0)}, bytes.NewReader(make([]byte, 64)))
	store := &resetStore{}
	var auditActor, auditAction, auditResource string
	mux := webkit.New()
	SessionEndpoints{
		Sessions:       manager,
		Verifier:       LocalVerifier{Store: store, Passwords: resetPasswords{}},
		MaxBodyBytes:   1024,
		BootstrapToken: "bootstrap-token-123",
		Audit: func(_ context.Context, actor, action, resourceID string) error {
			auditActor, auditAction, auditResource = actor, action, resourceID
			return nil
		},
	}.Register(mux)
	body, _ := json.Marshal(map[string]string{"username": "admin", "newPassword": "new-pass-1"})

	remote := httptest.NewRequest(http.MethodPost, "/api/admin/password/reset", bytes.NewReader(body))
	remote.RemoteAddr = "203.0.113.10:1234"
	remoteResponse := httptest.NewRecorder()
	mux.ServeHTTP(remoteResponse, remote)
	if remoteResponse.Code != http.StatusForbidden {
		t.Fatalf("remote reset status = %d", remoteResponse.Code)
	}

	// Loopback alone is not authorization: no session and no token is denied.
	anonymous := httptest.NewRequest(http.MethodPost, "/api/admin/password/reset", bytes.NewReader(body))
	anonymous.RemoteAddr = "127.0.0.1:1234"
	anonymousResponse := httptest.NewRecorder()
	mux.ServeHTTP(anonymousResponse, anonymous)
	if anonymousResponse.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous loopback reset status = %d", anonymousResponse.Code)
	}
	if store.hash != "" {
		t.Fatal("anonymous loopback reset must not change the password")
	}

	// The sessionless emergency path is credentialed by the bootstrap token.
	token := httptest.NewRequest(http.MethodPost, "/api/admin/password/reset", bytes.NewReader(body))
	token.RemoteAddr = "127.0.0.1:1234"
	token.Header.Set("X-Bootstrap-Token", "bootstrap-token-123")
	tokenResponse := httptest.NewRecorder()
	mux.ServeHTTP(tokenResponse, token)
	if tokenResponse.Code != http.StatusOK || store.username != "admin" || store.hash != "hash:new-pass-1" {
		t.Fatalf("token reset status=%d username=%q hash=%q", tokenResponse.Code, store.username, store.hash)
	}
	if auditActor != "bootstrap" || auditAction != "local_password.emergency_reset" || auditResource != "admin" {
		t.Fatalf("audit = %q %q %q", auditActor, auditAction, auditResource)
	}
}

func TestResetPasswordWithAdminSession(t *testing.T) {
	manager, _ := NewSessionManager(SessionConfig{CookieName: "lia", TTL: time.Hour, Secure: true}, &sessionClock{now: time.Unix(1, 0)}, bytes.NewReader(make([]byte, 64)))
	store := &resetStore{}
	var auditActor string
	mux := webkit.New()
	SessionEndpoints{
		Sessions:     manager,
		Verifier:     LocalVerifier{Store: store, Passwords: resetPasswords{}},
		MaxBodyBytes: 1024,
		Audit: func(_ context.Context, actor, _, _ string) error {
			auditActor = actor
			return nil
		},
	}.Register(mux)

	loginBody, _ := json.Marshal(map[string]string{"username": "admin", "password": "old-password"})
	loginResponse := httptest.NewRecorder()
	mux.ServeHTTP(loginResponse, httptest.NewRequest(http.MethodPost, "/api/admin/session", bytes.NewReader(loginBody)))
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d", loginResponse.Code)
	}
	var loginResult struct {
		CSRFToken string `json:"csrfToken"`
	}
	cookies := loginResponse.Result().Cookies()
	if json.Unmarshal(loginResponse.Body.Bytes(), &loginResult) != nil || len(cookies) != 1 {
		t.Fatalf("login response invalid: %s", loginResponse.Body.String())
	}

	body, _ := json.Marshal(map[string]string{"username": "admin", "newPassword": "new-pass-2"})
	// Session without CSRF is still denied (POST requires the token).
	noCSRF := httptest.NewRequest(http.MethodPost, "/api/admin/password/reset", bytes.NewReader(body))
	noCSRF.RemoteAddr = "127.0.0.1:1234"
	noCSRF.AddCookie(cookies[0])
	noCSRFResponse := httptest.NewRecorder()
	mux.ServeHTTP(noCSRFResponse, noCSRF)
	if noCSRFResponse.Code != http.StatusUnauthorized {
		t.Fatalf("session without CSRF status = %d", noCSRFResponse.Code)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/admin/password/reset", bytes.NewReader(body))
	request.RemoteAddr = "127.0.0.1:1234"
	request.AddCookie(cookies[0])
	request.Header.Set("X-CSRF-Token", loginResult.CSRFToken)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK || store.username != "admin" || store.hash != "hash:new-pass-2" {
		t.Fatalf("session reset status=%d username=%q hash=%q", response.Code, store.username, store.hash)
	}
	if auditActor != "admin" {
		t.Fatalf("audit actor = %q", auditActor)
	}
}

type strictPasswords struct{}

func (strictPasswords) Hash(password []byte) (string, error) { return "hash:" + string(password), nil }
func (strictPasswords) Verify(password []byte, stored string) (bool, error) {
	return "hash:"+string(password) == stored, nil
}

func TestChangePasswordRequiresSessionAndOldPassword(t *testing.T) {
	manager, _ := NewSessionManager(SessionConfig{CookieName: "lia", TTL: time.Hour, Secure: true}, &sessionClock{now: time.Unix(1, 0)}, bytes.NewReader(make([]byte, 64)))
	store := &resetStore{hash: "hash:old-password"}
	mux := webkit.New()
	SessionEndpoints{Sessions: manager, Verifier: LocalVerifier{Store: store, Passwords: strictPasswords{}}, MaxBodyBytes: 1024}.Register(mux)

	loginBody, _ := json.Marshal(map[string]string{"username": "admin", "password": "old-password"})
	loginResponse := httptest.NewRecorder()
	mux.ServeHTTP(loginResponse, httptest.NewRequest(http.MethodPost, "/api/admin/session", bytes.NewReader(loginBody)))
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d", loginResponse.Code)
	}
	var loginResult struct {
		CSRFToken string `json:"csrfToken"`
	}
	cookies := loginResponse.Result().Cookies()
	if json.Unmarshal(loginResponse.Body.Bytes(), &loginResult) != nil || len(cookies) != 1 {
		t.Fatalf("login response invalid: %s", loginResponse.Body.String())
	}

	unauthorized := httptest.NewRecorder()
	mux.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/api/admin/password/change", strings.NewReader("{}")))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("without session status = %d", unauthorized.Code)
	}

	doChange := func(oldPassword, newPassword string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"oldPassword": oldPassword, "newPassword": newPassword})
		request := httptest.NewRequest(http.MethodPost, "/api/admin/password/change", bytes.NewReader(body))
		request.AddCookie(cookies[0])
		request.Header.Set("X-CSRF-Token", loginResult.CSRFToken)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		return response
	}

	wrong := doChange("not-the-password", "new-password-1")
	if wrong.Code != http.StatusUnauthorized || !strings.Contains(wrong.Body.String(), "WRONG_PASSWORD") {
		t.Fatalf("wrong old password status=%d body=%s", wrong.Code, wrong.Body.String())
	}

	changed := doChange("old-password", "new-password-1")
	if changed.Code != http.StatusOK || store.username != "admin" || store.hash != "hash:new-password-1" {
		t.Fatalf("change status=%d body=%s store=%+v", changed.Code, changed.Body.String(), store)
	}
}
