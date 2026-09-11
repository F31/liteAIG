package sqlrepo

import (
	"context"
	"database/sql"
	"errors"

	"github.com/F31/liteAIG/internal/identity"
)

// PepperStore persists key peppers in the identity-owned tables.
type PepperStore struct{ db *sql.DB }

func NewPepperStore(db *sql.DB) *PepperStore { return &PepperStore{db: db} }

func (s *PepperStore) LoadCiphertext(ctx context.Context, version int) ([]byte, error) {
	var ciphertext []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT ciphertext FROM key_pepper_secrets WHERE version=$1`, version,
	).Scan(&ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, identity.ErrPepperNotPersisted
	}
	return ciphertext, err
}

func (s *PepperStore) SaveCiphertext(ctx context.Context, version int, ciphertext []byte) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO key_pepper_secrets(version, ciphertext) VALUES ($1, $2)
ON CONFLICT(version) DO NOTHING`, version, ciphertext)
	return err
}

func (s *PepperStore) MarkActive(ctx context.Context, version int, pepperRef string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO key_pepper_versions(version, pepper_ref, status)
VALUES ($1, $2, 'active')
ON CONFLICT(version) DO UPDATE SET status = 'active'`,
		version, pepperRef,
	)
	return err
}

var _ identity.PepperStore = (*PepperStore)(nil)
