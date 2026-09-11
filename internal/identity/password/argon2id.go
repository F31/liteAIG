// Package password implements local credential hashing without storing plaintext.
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
)

type Argon2idParams struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltBytes   uint32
	KeyBytes    uint32
}

func DefaultArgon2idParams() Argon2idParams {
	return Argon2idParams{
		MemoryKiB:   64 * 1024,
		Iterations:  3,
		Parallelism: 2,
		SaltBytes:   16,
		KeyBytes:    32,
	}
}

func (p Argon2idParams) validate() error {
	if p.MemoryKiB == 0 || p.Iterations == 0 || p.Parallelism == 0 || p.SaltBytes == 0 || p.KeyBytes == 0 {
		return errors.New("all Argon2id parameters must be positive")
	}
	return nil
}

type Argon2id struct {
	params Argon2idParams
	random io.Reader
}

func NewArgon2id(params Argon2idParams, random io.Reader) (*Argon2id, error) {
	if err := params.validate(); err != nil {
		return nil, err
	}
	if random == nil {
		random = rand.Reader
	}
	return &Argon2id{params: params, random: random}, nil
}

func (h *Argon2id) Hash(plaintext []byte) (string, error) {
	if len(plaintext) == 0 {
		return "", errors.New("password is required")
	}
	salt := make([]byte, h.params.SaltBytes)
	if _, err := io.ReadFull(h.random, salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey(
		plaintext,
		salt,
		h.params.Iterations,
		h.params.MemoryKiB,
		h.params.Parallelism,
		h.params.KeyBytes,
	)
	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		h.params.MemoryKiB,
		h.params.Iterations,
		h.params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
	clear(salt)
	clear(key)
	return encoded, nil
}

func (h *Argon2id) Verify(plaintext []byte, encoded string) (bool, error) {
	params, salt, expected, err := parse(encoded)
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey(
		plaintext,
		salt,
		params.Iterations,
		params.MemoryKiB,
		params.Parallelism,
		uint32(len(expected)),
	)
	matched := subtle.ConstantTimeCompare(actual, expected) == 1
	clear(actual)
	clear(expected)
	clear(salt)
	return matched, nil
}

func parse(encoded string) (Argon2idParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Argon2idParams{}, nil, nil, errors.New("invalid Argon2id encoding")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Argon2idParams{}, nil, nil, errors.New("unsupported Argon2id version")
	}
	var params Argon2idParams
	if _, err := fmt.Sscanf(
		parts[3],
		"m=%d,t=%d,p=%d",
		&params.MemoryKiB,
		&params.Iterations,
		&params.Parallelism,
	); err != nil {
		return Argon2idParams{}, nil, nil, errors.New("invalid Argon2id parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return Argon2idParams{}, nil, nil, errors.New("invalid Argon2id salt")
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) == 0 {
		return Argon2idParams{}, nil, nil, errors.New("invalid Argon2id key")
	}
	params.SaltBytes = uint32(len(salt))
	params.KeyBytes = uint32(len(expected))
	if err := params.validateForVerify(); err != nil {
		return Argon2idParams{}, nil, nil, err
	}
	return params, salt, expected, nil
}

func (p Argon2idParams) validateForVerify() error {
	if err := p.validate(); err != nil {
		return err
	}
	// Bound values read from storage so a corrupted hash cannot exhaust the process.
	if p.MemoryKiB > 1024*1024 || p.Iterations > 20 || p.Parallelism > 32 {
		return errors.New("Argon2id parameters exceed verification limits")
	}
	return nil
}
