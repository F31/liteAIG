package identity

import (
	"context"
	"errors"
	"time"

	"github.com/F31/liteAIG/internal/controlplane/rbac"
)

var (
	ErrLocalUserNotFound = errors.New("local user not found")
	ErrUsernameTaken     = errors.New("username already exists")
)

// LocalCredential is one local console account as seen by authentication.
type LocalCredential struct {
	AdminID      string
	TenantID     string
	Username     string
	Role         string
	PasswordHash string
	Status       string
	// Email is the address password-reset codes are delivered to; empty means
	// the email reset flow is unavailable for this account.
	Email string
}

// RoleOrDefault normalizes a legacy credential that predates role tracking.
func (c LocalCredential) RoleOrDefault() string {
	if c.Role == "" {
		return rbac.RoleTenantAdmin
	}
	return c.Role
}

// LocalUser is one local console account in the user directory.
type LocalUser struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"createdAt"`
	// PasswordHash is set by the caller only when creating an account and is
	// never serialized into API responses.
	PasswordHash string `json:"-"`
}

// LocalCredentialStore persists local console accounts (local_admins table).
type LocalCredentialStore interface {
	FindLocalCredential(context.Context, string) (LocalCredential, error)
	FindLocalUserByID(context.Context, string) (LocalCredential, error)
	ResetLocalPassword(context.Context, string, string) error
	ListLocalUsers(context.Context) ([]LocalUser, error)
	CreateLocalUser(context.Context, LocalUser) (LocalUser, error)
	SetUserRole(context.Context, string, string) (LocalUser, error)
	SetUserStatus(context.Context, string, string) (LocalUser, error)
	SetUserEmail(context.Context, string, string) (LocalUser, error)
	DeleteLocalUser(context.Context, string) error
	CountActiveAdmins(context.Context) (int, error)
}

// PasswordVerifier hashes and verifies local account passwords.
type PasswordVerifier interface {
	Hash([]byte) (string, error)
	Verify([]byte, string) (bool, error)
}
