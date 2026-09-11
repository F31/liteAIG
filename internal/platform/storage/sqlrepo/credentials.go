package sqlrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ErrSecretNotFound reports a missing or stale secret vault entry.
var ErrSecretNotFound = errors.New("secret material not found")

// SecretVault is a generic envelope-encrypted secret store keyed by opaque
// reference. The ciphertext is opaque to the vault; the app layer owns the
// cipher.
type SecretVault struct{ db *sql.DB }

func NewSecretVault(db *sql.DB) *SecretVault { return &SecretVault{db: db} }

// Put inserts or replaces the material for a reference.
func (s *SecretVault) Put(ctx context.Context, ref, tenantID string, ciphertext []byte) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO secret_material(secret_ref, tenant_id, ciphertext)
VALUES ($1, $2, $3)
ON CONFLICT(secret_ref) DO UPDATE SET ciphertext=EXCLUDED.ciphertext, status='enabled'`,
		ref, sql.NullString{String: tenantID, Valid: tenantID != ""}, ciphertext)
	return err
}

// Get returns the ciphertext stored under ref.
func (s *SecretVault) Get(ctx context.Context, ref string) ([]byte, error) {
	var ciphertext []byte
	err := s.db.QueryRowContext(ctx, `
SELECT ciphertext FROM secret_material WHERE secret_ref=$1 AND status='enabled'`, ref).Scan(&ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSecretNotFound
	}
	return ciphertext, err
}

// Rotate marks a reference's material rotated (the caller re-Puts new bytes).
func (s *SecretVault) Rotate(ctx context.Context, ref string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE secret_material SET status='rotated', rotated_at=CURRENT_TIMESTAMP WHERE secret_ref=$1`, ref)
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return ErrSecretNotFound
	}
	return nil
}

// Replace atomically rotates the material for a reference: it overwrites the
// ciphertext and marks the superseded material rotated in the same statement.
// Unlike a Rotate-then-Put sequence there is no crash window in which the
// reference is unreadable.
func (s *SecretVault) Replace(ctx context.Context, ref, tenantID string, ciphertext []byte) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO secret_material(secret_ref, tenant_id, ciphertext)
VALUES ($1, $2, $3)
ON CONFLICT(secret_ref) DO UPDATE
  SET ciphertext=EXCLUDED.ciphertext, status='enabled', rotated_at=CURRENT_TIMESTAMP`,
		ref, sql.NullString{String: tenantID, Valid: tenantID != ""}, ciphertext)
	return err
}

// Disable revokes a reference.
func (s *SecretVault) Disable(ctx context.Context, ref string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE secret_material SET status='disabled' WHERE secret_ref=$1`, ref)
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return ErrSecretNotFound
	}
	return nil
}

// SecretResolver resolves local://credential/<id> references by decrypting the
// vault ciphertext with the supplied opener (the app's envelope cipher).
type SecretResolver struct {
	vault  *SecretVault
	open   func([]byte) ([]byte, error)
	prefix string
}

// NewSecretResolver builds a resolver over the vault and cipher opener.
func NewSecretResolver(vault *SecretVault, open func([]byte) ([]byte, error)) *SecretResolver {
	return &SecretResolver{vault: vault, open: open, prefix: "local://credential/"}
}

// Resolve decrypts the referenced credential.
func (r *SecretResolver) Resolve(ctx context.Context, ref string) ([]byte, error) {
	id, ok := strings.CutPrefix(ref, r.prefix)
	if !ok || id == "" {
		return nil, fmt.Errorf("unknown secret reference %q", ref)
	}
	ciphertext, err := r.vault.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	return r.open(ciphertext)
}
