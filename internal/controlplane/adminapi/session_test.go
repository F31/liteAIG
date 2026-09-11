package adminapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type sessionClock struct{ now time.Time }

func (c *sessionClock) Now() time.Time { return c.now }
func TestSecureSessionAndCSRF(t *testing.T) {
	clock := &sessionClock{now: time.Unix(1, 0)}
	manager, err := NewSessionManager(SessionConfig{CookieName: "lia_session", TTL: time.Hour, Secure: true}, clock, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	csrf, err := manager.Create(response, Session{AdminID: "admin", TenantID: "tenant"})
	if err != nil {
		t.Fatal(err)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie=%+v", cookies)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/admin/keys", nil)
	request.AddCookie(cookies[0])
	if _, err := manager.Authorize(request); err == nil {
		t.Fatal("write authorized without CSRF")
	}
	request.Header.Set("X-CSRF-Token", csrf)
	session, err := manager.Authorize(request)
	if err != nil || session.TenantID != "tenant" {
		t.Fatalf("Authorize()=%+v,%v", session, err)
	}
}

func TestSwitchTenantUpdatesStoredSession(t *testing.T) {
	clock := &sessionClock{now: time.Unix(1, 0)}
	manager, err := NewSessionManager(SessionConfig{CookieName: "lia_session", TTL: time.Hour, Secure: true}, clock, bytes.NewReader(make([]byte, 96)))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	csrf, err := manager.Create(response, Session{AdminID: "root", Role: "system_admin"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/admin/session/tenant", nil)
	request.AddCookie(response.Result().Cookies()[0])
	request.Header.Set("X-CSRF-Token", csrf)
	if _, err := manager.Authorize(request); err != nil {
		t.Fatalf("Authorize before switch = %v", err)
	}
	if session, err := manager.SwitchTenant(request, "tenant-1"); err != nil || session.TenantID != "tenant-1" {
		t.Fatalf("SwitchTenant()=%+v,%v", session, err)
	}
	if session, err := manager.Authorize(request); err != nil || session.TenantID != "tenant-1" {
		t.Fatalf("Authorize after switch = %+v,%v", session, err)
	}
}
