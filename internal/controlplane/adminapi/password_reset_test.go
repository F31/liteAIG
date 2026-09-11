package adminapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/platform/webkit"
)

type resetServiceFake struct {
	enabled     bool
	forgotErr   error
	confirmErr  error
	sends       int
	forgotUser  string
	confirmUser string
	confirmCode string
}

func (f *resetServiceFake) Enabled() bool { return f.enabled }
func (f *resetServiceFake) Forgot(_ context.Context, username string) error {
	f.sends++
	f.forgotUser = username
	return f.forgotErr
}
func (f *resetServiceFake) Confirm(_ context.Context, username, code string, _ []byte) error {
	f.confirmUser = username
	f.confirmCode = code
	return f.confirmErr
}

func TestPasswordResetConfigEndpoint(t *testing.T) {
	without := webkit.New()
	SessionEndpoints{MaxBodyBytes: 1024}.Register(without)
	response := httptest.NewRecorder()
	without.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/admin/password/forgot/config", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"enabled":false`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	with := webkit.New()
	SessionEndpoints{MaxBodyBytes: 1024, PasswordReset: &resetServiceFake{enabled: true}}.Register(with)
	response = httptest.NewRecorder()
	with.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/admin/password/forgot/config", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"enabled":true`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestForgotPasswordEndpoint(t *testing.T) {
	fake := &resetServiceFake{enabled: true}
	mux := webkit.New()
	SessionEndpoints{
		MaxBodyBytes:     1024,
		PasswordReset:    fake,
		ResetRateLimiter: NewResetRateLimiter(15*time.Minute, 10, 5, time.Minute),
	}.Register(mux)

	send := func(username string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"username": username})
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/admin/password/forgot", bytes.NewReader(body)))
		return response
	}
	if response := send("alice"); response.Code != http.StatusAccepted || fake.sends != 1 || fake.forgotUser != "alice" {
		t.Fatalf("status=%d body=%s sends=%d", response.Code, response.Body.String(), fake.sends)
	}

	empty := httptest.NewRecorder()
	mux.ServeHTTP(empty, httptest.NewRequest(http.MethodPost, "/api/admin/password/forgot", strings.NewReader(`{}`)))
	if empty.Code != http.StatusBadRequest {
		t.Fatalf("empty username status = %d", empty.Code)
	}

	fake.forgotErr = errors.New("smtp down")
	failed := send("bob")
	if failed.Code != http.StatusBadGateway || !strings.Contains(failed.Body.String(), "RESET_EMAIL_FAILED") {
		t.Fatalf("send failure status=%d body=%s", failed.Code, failed.Body.String())
	}
}

func TestForgotPasswordRateLimited(t *testing.T) {
	fake := &resetServiceFake{enabled: true}
	mux := webkit.New()
	SessionEndpoints{
		MaxBodyBytes:     1024,
		PasswordReset:    fake,
		ResetRateLimiter: NewResetRateLimiter(15*time.Minute, 3, 2, time.Minute),
	}.Register(mux)

	send := func(username string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"username": username})
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/admin/password/forgot", bytes.NewReader(body)))
		return response
	}

	if response := send("alice"); response.Code != http.StatusAccepted {
		t.Fatalf("first send status = %d body=%s", response.Code, response.Body.String())
	}
	// A second send for the same username inside the minimum gap is denied.
	if response := send("alice"); response.Code != http.StatusTooManyRequests || !strings.Contains(response.Body.String(), "RATE_LIMITED") {
		t.Fatalf("interval status=%d body=%s", response.Code, response.Body.String())
	}
	if response := send("bob"); response.Code != http.StatusAccepted {
		t.Fatalf("bob status = %d", response.Code)
	}
	if response := send("carol"); response.Code != http.StatusAccepted {
		t.Fatalf("carol status = %d", response.Code)
	}
	// The client IP is now at its cap: the next username is denied.
	if response := send("dave"); response.Code != http.StatusTooManyRequests {
		t.Fatalf("IP cap status = %d body=%s", response.Code, response.Body.String())
	}
	if fake.sends != 3 {
		t.Fatalf("sends = %d, want 3", fake.sends)
	}
}

func TestResetRateLimiterCaps(t *testing.T) {
	base := time.Now()
	limiter := &ResetRateLimiter{
		window: 15 * time.Minute, maxPerIP: 3, maxPerAccount: 2,
		ipHits: map[string][]time.Time{}, acctHits: map[string][]time.Time{}, lastSent: map[string]time.Time{},
	}
	if !limiter.AllowSend("1.1.1.1", "alice", base) ||
		!limiter.AllowSend("1.1.1.1", "alice", base.Add(time.Second)) {
		t.Fatal("allowed sends were denied")
	}
	if limiter.AllowSend("1.1.1.1", "alice", base.Add(2*time.Second)) {
		t.Fatal("per-account cap not enforced")
	}
	if !limiter.AllowSend("1.1.1.1", "bob", base.Add(3*time.Second)) {
		t.Fatal("second IP send denied")
	}
	if limiter.AllowSend("1.1.1.1", "carol", base.Add(4*time.Second)) {
		t.Fatal("per-IP cap not enforced")
	}
	if !limiter.AllowSend("2.2.2.2", "alice", base.Add(5*time.Second)) {
		t.Fatal("unrelated client blocked")
	}

	gap := &ResetRateLimiter{
		window: 15 * time.Minute, maxPerIP: 10, maxPerAccount: 5, minInterval: time.Minute,
		ipHits: map[string][]time.Time{}, acctHits: map[string][]time.Time{}, lastSent: map[string]time.Time{},
	}
	if !gap.AllowSend("1.1.1.1", "alice", base) {
		t.Fatal("first send denied")
	}
	// The per-username gap is global: another IP cannot bypass it.
	if gap.AllowSend("2.2.2.2", "alice", base.Add(time.Second)) {
		t.Fatal("minimum gap not enforced across IPs")
	}
	if !gap.AllowSend("2.2.2.2", "alice", base.Add(2*time.Minute)) {
		t.Fatal("send after the gap denied")
	}
}

func TestConfirmPasswordResetEndpoint(t *testing.T) {
	fake := &resetServiceFake{enabled: true}
	mux := webkit.New()
	SessionEndpoints{MaxBodyBytes: 1024, PasswordReset: fake}.Register(mux)
	body, _ := json.Marshal(map[string]string{"username": "alice", "code": "123456", "newPassword": "new-password-1"})

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/admin/password/reset/confirm", bytes.NewReader(body)))
	if response.Code != http.StatusOK || fake.confirmUser != "alice" || fake.confirmCode != "123456" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	fake.confirmErr = errors.New("bad code")
	failed := httptest.NewRecorder()
	mux.ServeHTTP(failed, httptest.NewRequest(http.MethodPost, "/api/admin/password/reset/confirm", bytes.NewReader(body)))
	if failed.Code != http.StatusUnauthorized || !strings.Contains(failed.Body.String(), "INVALID_CODE") {
		t.Fatalf("failure status=%d body=%s", failed.Code, failed.Body.String())
	}

	disabled := webkit.New()
	SessionEndpoints{MaxBodyBytes: 1024}.Register(disabled)
	off := httptest.NewRecorder()
	disabled.ServeHTTP(off, httptest.NewRequest(http.MethodPost, "/api/admin/password/reset/confirm", bytes.NewReader(body)))
	if off.Code != http.StatusUnauthorized || !strings.Contains(off.Body.String(), "INVALID_CODE") {
		t.Fatalf("disabled status=%d body=%s", off.Code, off.Body.String())
	}

	short, _ := json.Marshal(map[string]string{"username": "alice", "code": "123456", "newPassword": "short"})
	shortResponse := httptest.NewRecorder()
	mux.ServeHTTP(shortResponse, httptest.NewRequest(http.MethodPost, "/api/admin/password/reset/confirm", bytes.NewReader(short)))
	if shortResponse.Code != http.StatusBadRequest {
		t.Fatalf("short password status = %d", shortResponse.Code)
	}
}
