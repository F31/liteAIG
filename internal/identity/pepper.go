package identity

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
)

// ErrPepperNotPersisted is returned by PepperStore.LoadCiphertext when the
// pepper version has never been stored.
var ErrPepperNotPersisted = errors.New("key pepper not persisted")

// PepperStore persists the envelope-encrypted key pepper (identity-owned
// key_pepper_secrets / key_pepper_versions tables).
type PepperStore interface {
	// LoadCiphertext returns the stored ciphertext for a pepper version or
	// ErrPepperNotPersisted when the version has never been persisted.
	LoadCiphertext(context.Context, int) ([]byte, error)
	// SaveCiphertext stores the ciphertext for a version without overwriting
	// an existing entry.
	SaveCiphertext(context.Context, int, []byte) error
	// MarkActive records the version as the active pepper reference.
	MarkActive(context.Context, int, string) error
}

// Crypter envelope-encrypts and decrypts secret material.
type Crypter interface {
	Encrypt(plain []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

// Pepper is the active key pepper: a resolvable secret plus its persistence
// coordinates.
type Pepper struct {
	Version int
	Ref     string
	secret  []byte
}

// Resolve implements the secrets provider surface for the local pepper.
func (p *Pepper) Resolve(_ context.Context, ref string) ([]byte, error) {
	if ref != p.Ref {
		return nil, fmt.Errorf("unknown secret reference %q", ref)
	}
	return append([]byte(nil), p.secret...), nil
}

// EnsurePepper returns the active key pepper, loading the persisted ciphertext
// when present so virtual API keys survive restarts. On first boot a fresh
// pepper is generated, envelope-encrypted, and stored.
func EnsurePepper(ctx context.Context, store PepperStore, crypt Crypter, version int, ref string) (*Pepper, error) {
	if store == nil || crypt == nil {
		return nil, fmt.Errorf("pepper store and crypter are required")
	}
	stored, err := store.LoadCiphertext(ctx, version)
	if err == nil {
		plain, err := crypt.Decrypt(stored)
		if err != nil {
			return nil, fmt.Errorf("decrypt stored key pepper: %w", err)
		}
		if err := store.MarkActive(ctx, version, ref); err != nil {
			return nil, err
		}
		return &Pepper{Version: version, Ref: ref, secret: plain}, nil
	}
	if !errors.Is(err, ErrPepperNotPersisted) {
		return nil, fmt.Errorf("load key pepper: %w", err)
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate local key pepper: %w", err)
	}
	ciphertext, err := crypt.Encrypt(secret)
	if err != nil {
		return nil, fmt.Errorf("encrypt local key pepper: %w", err)
	}
	if err := store.SaveCiphertext(ctx, version, ciphertext); err != nil {
		return nil, fmt.Errorf("store local key pepper: %w", err)
	}
	if err := store.MarkActive(ctx, version, ref); err != nil {
		return nil, err
	}
	return &Pepper{Version: version, Ref: ref, secret: secret}, nil
}
