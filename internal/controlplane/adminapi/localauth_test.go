package adminapi

import (
	"context"
	"errors"
	"testing"
	"time"
)

type credentialStore struct {
	value LocalCredential
	users []LocalUser
}

func (s *credentialStore) FindLocalCredential(_ context.Context, username string) (LocalCredential, error) {
	if s.value.Username != "" && s.value.Username != username {
		return LocalCredential{}, errors.New("credential not found")
	}
	return s.value, nil
}
func (s *credentialStore) FindLocalUserByID(_ context.Context, id string) (LocalCredential, error) {
	if s.value.AdminID != id {
		return LocalCredential{}, ErrLocalUserNotFound
	}
	return s.value, nil
}
func (s *credentialStore) ResetLocalPassword(_ context.Context, _ string, hash string) error {
	s.value.PasswordHash = hash
	return nil
}
func (s *credentialStore) ListLocalUsers(context.Context) ([]LocalUser, error) {
	return s.users, nil
}
func (s *credentialStore) CreateLocalUser(_ context.Context, user LocalUser) (LocalUser, error) {
	for _, existing := range s.users {
		if existing.Username == user.Username {
			return LocalUser{}, ErrUsernameTaken
		}
	}
	user.Status = "active"
	user.CreatedAt = time.Unix(100, 0)
	s.users = append(s.users, user)
	return user, nil
}
func (s *credentialStore) SetUserRole(_ context.Context, id, role string) (LocalUser, error) {
	for i := range s.users {
		if s.users[i].ID == id {
			s.users[i].Role = role
			return s.users[i], nil
		}
	}
	return LocalUser{}, ErrLocalUserNotFound
}
func (s *credentialStore) SetUserStatus(_ context.Context, id, status string) (LocalUser, error) {
	for i := range s.users {
		if s.users[i].ID == id {
			s.users[i].Status = status
			return s.users[i], nil
		}
	}
	return LocalUser{}, ErrLocalUserNotFound
}
func (s *credentialStore) SetUserEmail(_ context.Context, id, email string) (LocalUser, error) {
	for i := range s.users {
		if s.users[i].ID == id {
			s.users[i].Email = email
			return s.users[i], nil
		}
	}
	return LocalUser{}, ErrLocalUserNotFound
}
func (s *credentialStore) DeleteLocalUser(_ context.Context, id string) error {
	for i := range s.users {
		if s.users[i].ID == id {
			s.users = append(s.users[:i], s.users[i+1:]...)
			return nil
		}
	}
	return ErrLocalUserNotFound
}
func (s *credentialStore) CountActiveAdmins(context.Context) (int, error) {
	count := 0
	for _, user := range s.users {
		if user.Role == "tenant_admin" && user.Status == "active" {
			count++
		}
	}
	return count, nil
}

type passwordVerifier struct{ match bool }

func (v passwordVerifier) Hash(password []byte) (string, error) {
	return "hash:" + string(password), nil
}
func (v passwordVerifier) Verify(password []byte, stored string) (bool, error) {
	if !v.match {
		return false, nil
	}
	return "hash:"+string(password) == stored, nil
}

func TestLocalVerifierReturnsBoundTenantWithoutHash(t *testing.T) {
	verifier := LocalVerifier{Store: &credentialStore{value: LocalCredential{AdminID: "admin", TenantID: "tenant", Username: "admin", Role: "viewer", PasswordHash: "hash:password", Status: "active"}}, Passwords: passwordVerifier{true}}
	session, err := verifier.Verify(context.Background(), "admin", []byte("password"))
	if err != nil || session.AdminID != "admin" || session.TenantID != "tenant" {
		t.Fatalf("Verify()=%+v,%v", session, err)
	}
	if session.Username != "admin" || session.Role != "viewer" || session.AuthMethod != "local" {
		t.Fatalf("Verify() session identity = %+v, want username=admin role=viewer", session)
	}
	verifier.Passwords = passwordVerifier{false}
	if _, err := verifier.Verify(context.Background(), "admin", []byte("wrong")); err == nil {
		t.Fatal("wrong password was accepted")
	}
}

func TestLocalVerifierDefaultsLegacyRoleToTenantAdmin(t *testing.T) {
	verifier := LocalVerifier{Store: &credentialStore{value: LocalCredential{AdminID: "admin", TenantID: "tenant", Username: "admin", PasswordHash: "hash:password", Status: "active"}}, Passwords: passwordVerifier{true}}
	session, err := verifier.Verify(context.Background(), "admin", []byte("password"))
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if session.Role != "tenant_admin" {
		t.Fatalf("legacy credential role = %q, want tenant_admin", session.Role)
	}
}
