package app

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/controlplane/adminapi"
	"github.com/F31/liteAIG/internal/identity"
)

type resetMail struct {
	err     error
	sends   int
	to      string
	subject string
	body    string
}

func (m *resetMail) Send(_ context.Context, to, subject, body string) error {
	m.sends++
	if m.err != nil {
		return m.err
	}
	m.to, m.subject, m.body = to, subject, body
	return nil
}

type resetUsers struct {
	users map[string]identity.LocalCredential
	hash  string
}

func (u *resetUsers) FindLocalCredential(_ context.Context, username string) (identity.LocalCredential, error) {
	if credential, ok := u.users[username]; ok {
		return credential, nil
	}
	return identity.LocalCredential{}, identity.ErrLocalUserNotFound
}

func (u *resetUsers) FindLocalUserByID(_ context.Context, adminID string) (identity.LocalCredential, error) {
	for _, credential := range u.users {
		if credential.AdminID == adminID {
			return credential, nil
		}
	}
	return identity.LocalCredential{}, identity.ErrLocalUserNotFound
}

func (u *resetUsers) ResetLocalPassword(_ context.Context, username, hash string) error {
	u.hash = hash
	if credential, ok := u.users[username]; ok {
		credential.PasswordHash = hash
		u.users[username] = credential
	}
	return nil
}

func (u *resetUsers) ListLocalUsers(context.Context) ([]identity.LocalUser, error) {
	return nil, nil
}
func (u *resetUsers) CreateLocalUser(context.Context, identity.LocalUser) (identity.LocalUser, error) {
	return identity.LocalUser{}, identity.ErrUsernameTaken
}
func (u *resetUsers) SetUserRole(context.Context, string, string) (identity.LocalUser, error) {
	return identity.LocalUser{}, identity.ErrLocalUserNotFound
}
func (u *resetUsers) SetUserStatus(context.Context, string, string) (identity.LocalUser, error) {
	return identity.LocalUser{}, identity.ErrLocalUserNotFound
}
func (u *resetUsers) SetUserEmail(context.Context, string, string) (identity.LocalUser, error) {
	return identity.LocalUser{}, identity.ErrLocalUserNotFound
}
func (u *resetUsers) DeleteLocalUser(context.Context, string) error {
	return identity.ErrLocalUserNotFound
}
func (u *resetUsers) CountActiveAdmins(context.Context) (int, error) { return 1, nil }

type resetHasher struct{}

func (resetHasher) Hash(password []byte) (string, error) { return "hash:" + string(password), nil }
func (resetHasher) Verify(password []byte, stored string) (bool, error) {
	return "hash:"+string(password) == stored, nil
}

func newResetServiceTest(t *testing.T) (*passwordResetService, *resetMail, *resetUsers, *mutableClock) {
	t.Helper()
	users := &resetUsers{users: map[string]identity.LocalCredential{
		"alice": {AdminID: "admin-alice", TenantID: "tenant", Username: "alice", Role: "admin", Status: "active", Email: "alice@example.com"},
		"bob":   {AdminID: "admin-bob", TenantID: "tenant", Username: "bob", Role: "admin", Status: "active", Email: ""},
		"carol": {AdminID: "admin-carol", TenantID: "tenant", Username: "carol", Role: "admin", Status: "disabled", Email: "carol@example.com"},
	}}
	mail := &resetMail{}
	clock := &mutableClock{now: time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)}
	manager, err := adminapi.NewSessionManager(adminapi.SessionConfig{CookieName: "lia", TTL: time.Hour, Secure: true}, clock, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	service := newPasswordResetService(users, resetHasher{}, mail, manager,
		func(context.Context, string, string, string) error { return nil }, clock)
	return service, mail, users, clock
}

var resetCodePattern = regexp.MustCompile(`\b(\d{6})\b`)

func issuedCode(t *testing.T, mail *resetMail) string {
	t.Helper()
	code := resetCodePattern.FindString(mail.body)
	if code == "" {
		t.Fatalf("no 6-digit code in mail body %q", mail.body)
	}
	return code
}

func TestPasswordResetEmailLoop(t *testing.T) {
	service, mail, users, _ := newResetServiceTest(t)
	ctx := context.Background()

	if err := service.Forgot(ctx, "alice"); err != nil {
		t.Fatalf("forgot = %v", err)
	}
	if mail.sends != 1 || mail.to != "alice@example.com" {
		t.Fatalf("mail = sends=%d to=%q", mail.sends, mail.to)
	}
	code := issuedCode(t, mail)

	if err := service.Confirm(ctx, "alice", "000000", []byte("new-password-1")); err == nil {
		t.Fatal("wrong code accepted")
	}

	recorder := httptest.NewRecorder()
	if _, err := service.sessions.Create(recorder, adminapi.Session{AdminID: "admin-alice", TenantID: "tenant", Username: "alice"}); err != nil {
		t.Fatalf("create session = %v", err)
	}
	request := httptest.NewRequest("GET", "/", nil)
	request.AddCookie(recorder.Result().Cookies()[0])
	if _, err := service.sessions.Authorize(request); err != nil {
		t.Fatalf("session should be valid before the reset: %v", err)
	}

	if err := service.Confirm(ctx, "alice", code, []byte("new-password-1")); err != nil {
		t.Fatalf("confirm = %v", err)
	}
	if users.hash != "hash:new-password-1" {
		t.Fatalf("password hash = %q", users.hash)
	}
	if _, err := service.sessions.Authorize(request); err == nil {
		t.Fatal("pre-reset session still valid after the reset")
	}
	if err := service.Confirm(ctx, "alice", code, []byte("other-password")); err == nil {
		t.Fatal("single-use code was accepted twice")
	}
}

func TestPasswordResetHidesAccountState(t *testing.T) {
	service, mail, _, _ := newResetServiceTest(t)
	ctx := context.Background()

	for _, username := range []string{"ghost", "bob", "carol"} {
		if err := service.Forgot(ctx, username); err != nil {
			t.Fatalf("forgot(%q) = %v", username, err)
		}
	}
	if mail.sends != 0 {
		t.Fatalf("mail sends for hidden accounts = %d", mail.sends)
	}
	if err := service.Confirm(ctx, "ghost", "123456", []byte("new-password-1")); err == nil {
		t.Fatal("confirm succeeded for an account that never received a code")
	}
}

func TestPasswordResetMailFailureDropsCode(t *testing.T) {
	service, mail, _, _ := newResetServiceTest(t)
	ctx := context.Background()
	mail.err = errors.New("smtp down")

	if err := service.Forgot(ctx, "alice"); err == nil {
		t.Fatal("forgot succeeded although delivery failed")
	}
	if err := service.Confirm(ctx, "alice", "000000", []byte("new-password-1")); err == nil {
		t.Fatal("undelivered code could be confirmed")
	}
}

func TestPasswordResetWrongCodeAttemptsLockOut(t *testing.T) {
	service, mail, _, _ := newResetServiceTest(t)
	ctx := context.Background()
	if err := service.Forgot(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	code := issuedCode(t, mail)
	wrong := "000000"
	if wrong == code {
		wrong = "000001"
	}
	for i := 0; i < resetCodeMaxTries; i++ {
		if err := service.Confirm(ctx, "alice", wrong, []byte("new-password-1")); err == nil {
			t.Fatal("wrong code accepted")
		}
	}
	if err := service.Confirm(ctx, "alice", code, []byte("new-password-1")); err == nil {
		t.Fatal("code still usable after the attempt limit")
	}
}

func TestPasswordResetCodeExpires(t *testing.T) {
	service, mail, _, clock := newResetServiceTest(t)
	ctx := context.Background()
	if err := service.Forgot(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	code := issuedCode(t, mail)
	clock.now = clock.now.Add(resetCodeTTL + time.Minute)
	if err := service.Confirm(ctx, "alice", code, []byte("new-password-1")); err == nil {
		t.Fatal("expired code accepted")
	}
}

func TestPasswordResetDisabledWithoutMail(t *testing.T) {
	users := &resetUsers{users: map[string]identity.LocalCredential{
		"alice": {AdminID: "admin-alice", TenantID: "tenant", Username: "alice", Role: "admin", Status: "active", Email: "alice@example.com"},
	}}
	service := newPasswordResetService(users, resetHasher{}, nil, nil, nil, &mutableClock{})
	ctx := context.Background()
	if service.Enabled() {
		t.Fatal("service enabled without a mail sender")
	}
	if err := service.Forgot(ctx, "alice"); err != nil {
		t.Fatalf("forgot = %v", err)
	}
	if err := service.Confirm(ctx, "alice", "123456", []byte("new-password-1")); err == nil {
		t.Fatal("confirm succeeded while disabled")
	}
}
