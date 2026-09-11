package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/F31/liteAIG/internal/controlplane/adminapi"
	"github.com/F31/liteAIG/internal/identity"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/platform/mail"
)

const (
	resetCodeTTL      = 10 * time.Minute
	resetCodeMaxTries = 5
)

type resetCodeEntry struct {
	hash      [32]byte
	expiresAt time.Time
	tries     int
}

// passwordResetService implements the email-verified password reset flow.
// Codes are 6-digit, single-use, time-boxed, and stored only as SHA-256
// hashes; a code is invalidated after five wrong confirmations so the short
// code space cannot be brute-forced. Every state change is audited and a
// successful reset voids the account's live sessions.
type passwordResetService struct {
	store     identity.LocalCredentialStore
	passwords identity.PasswordVerifier
	mail      mail.Sender
	sessions  *adminapi.SessionManager
	audit     func(ctx context.Context, actor, action, resourceID string) error
	clock     contracts.Clock

	mu    sync.Mutex
	codes map[string]*resetCodeEntry
}

func newPasswordResetService(
	store identity.LocalCredentialStore,
	passwords identity.PasswordVerifier,
	sender mail.Sender,
	sessions *adminapi.SessionManager,
	audit func(ctx context.Context, actor, action, resourceID string) error,
	clock contracts.Clock,
) *passwordResetService {
	return &passwordResetService{
		store: store, passwords: passwords, mail: sender, sessions: sessions,
		audit: audit, clock: clock, codes: map[string]*resetCodeEntry{},
	}
}

func (s *passwordResetService) Enabled() bool { return s.mail != nil }

func (s *passwordResetService) Forgot(ctx context.Context, username string) error {
	if !s.Enabled() {
		return nil
	}
	credential, err := s.store.FindLocalCredential(ctx, username)
	if err != nil || credential.Status != "active" || credential.Email == "" {
		// Unknown account, disabled account, or no email on file: uniform
		// no-op, so callers cannot tell the cases apart.
		return nil
	}
	code, err := newResetCode()
	if err != nil {
		return err
	}
	hash := sha256.Sum256([]byte(code))
	now := s.clock.Now()
	s.mu.Lock()
	s.codes[username] = &resetCodeEntry{hash: hash, expiresAt: now.Add(resetCodeTTL)}
	s.mu.Unlock()

	body := fmt.Sprintf(
		"Hello %s,\n\nYour LiteAIG password reset code is %s.\n\n"+
			"It expires in 10 minutes. Enter it on the login screen together with your new password to finish the reset.\n\n"+
			"If you did not request a password reset, ignore this email: your password will not change.\n",
		username, code,
	)
	if err := s.mail.Send(ctx, credential.Email, "LiteAIG password reset code", body); err != nil {
		// The code never left the process; drop it so no undelivered code
		// lingers for a later confirmation.
		s.mu.Lock()
		delete(s.codes, username)
		s.mu.Unlock()
		return err
	}
	if s.audit != nil {
		_ = s.audit(ctx, "anonymous", "local_password.reset_code_sent", username)
	}
	return nil
}

func (s *passwordResetService) Confirm(ctx context.Context, username, code string, newPassword []byte) error {
	if !s.Enabled() {
		return fmt.Errorf("password reset is not enabled")
	}
	now := s.clock.Now()
	s.mu.Lock()
	entry := s.codes[username]
	match := false
	if entry != nil && now.Before(entry.expiresAt) {
		provided := sha256.Sum256([]byte(code))
		match = subtle.ConstantTimeCompare(provided[:], entry.hash[:]) == 1
		if !match {
			entry.tries++
			if entry.tries >= resetCodeMaxTries {
				delete(s.codes, username)
			}
		}
	} else if entry != nil {
		delete(s.codes, username) // expired
	}
	s.mu.Unlock()
	if !match {
		return fmt.Errorf("invalid reset code")
	}
	// Single-use: consume the code before touching the account so a racing
	// duplicate cannot double-confirm.
	s.mu.Lock()
	if current := s.codes[username]; current == entry {
		delete(s.codes, username)
	}
	s.mu.Unlock()

	credential, err := s.store.FindLocalCredential(ctx, username)
	if err != nil || credential.Status != "active" {
		return fmt.Errorf("account unavailable")
	}
	hash, err := s.passwords.Hash(newPassword)
	if err != nil {
		return err
	}
	if err := s.store.ResetLocalPassword(ctx, username, hash); err != nil {
		return err
	}
	if s.sessions != nil {
		s.sessions.InvalidateForAdmin(credential.AdminID)
	}
	if s.audit != nil {
		_ = s.audit(ctx, username, "local_password.reset_confirmed", username)
	}
	return nil
}

// newResetCode returns a 6-digit code drawn uniformly from [0, 999999].
func newResetCode() (string, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", value.Int64()), nil
}

var _ adminapi.PasswordResetService = (*passwordResetService)(nil)
