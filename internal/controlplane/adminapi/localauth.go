package adminapi

import (
	"context"
	"errors"

	"github.com/F31/liteAIG/internal/identity"
)

// Local account types live in the identity domain; the Console API consumes
// them through aliases so business-layer signatures never reference the
// presentation package.
var (
	ErrLocalUserNotFound = identity.ErrLocalUserNotFound
	ErrUsernameTaken     = identity.ErrUsernameTaken
)

type (
	LocalCredential      = identity.LocalCredential
	LocalUser            = identity.LocalUser
	LocalCredentialStore = identity.LocalCredentialStore
	PasswordVerifier     = identity.PasswordVerifier
)

// LocalVerifier authenticates a username/password pair against the local
// credential store and produces a Console session.
type LocalVerifier struct {
	Store     LocalCredentialStore
	Passwords PasswordVerifier
}

func (v LocalVerifier) Verify(ctx context.Context, username string, password []byte) (Session, error) {
	if v.Store == nil || v.Passwords == nil || username == "" || len(password) == 0 {
		return Session{}, errors.New("invalid credentials")
	}
	credential, err := v.Store.FindLocalCredential(ctx, username)
	if err != nil || credential.Status != "active" {
		return Session{}, errors.New("invalid credentials")
	}
	matched, err := v.Passwords.Verify(password, credential.PasswordHash)
	if err != nil || !matched {
		return Session{}, errors.New("invalid credentials")
	}
	return Session{
		AdminID:    credential.AdminID,
		TenantID:   credential.TenantID,
		Username:   credential.Username,
		Role:       credential.RoleOrDefault(),
		AuthMethod: "local",
	}, nil
}

var _ CredentialVerifier = LocalVerifier{}
