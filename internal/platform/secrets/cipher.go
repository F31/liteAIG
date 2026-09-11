package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
)

// Cipher is an AES-256-GCM envelope cipher for secret material at rest.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher builds a Cipher from a 32-byte master key.
func NewCipher(masterKey []byte) (*Cipher, error) {
	if len(masterKey) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes, got %d", len(masterKey))
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, fmt.Errorf("build aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt seals plaintext; the result is nonce || ciphertext || tag.
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt opens data produced by Encrypt.
func (c *Cipher) Decrypt(data []byte) ([]byte, error) {
	size := c.aead.NonceSize()
	if len(data) < size {
		return nil, errors.New("ciphertext too short")
	}
	plaintext, err := c.aead.Open(nil, data[:size], data[size:], nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt secret: %w", err)
	}
	return plaintext, nil
}

// GenerateMasterKey returns a fresh random 32-byte master key.
func GenerateMasterKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	return key, nil
}

// LoadMasterKey resolves the envelope master key:
//  1. LITEAIG_MASTER_KEY env var (base64-encoded 32 bytes), when set;
//  2. the sidecar file <path>.masterkey (base64, mode 0600), created on first use;
//  3. a random ephemeral key (ephemeral in-memory databases only).
func LoadMasterKey(path string) ([]byte, error) {
	if env, ok := os.LookupEnv("LITEAIG_MASTER_KEY"); ok && env != "" {
		key, err := base64.StdEncoding.DecodeString(env)
		if err != nil {
			return nil, fmt.Errorf("LITEAIG_MASTER_KEY must be base64: %w", err)
		}
		return key, nil
	}
	if path != "" {
		sidecar := path + ".masterkey"
		if data, err := os.ReadFile(sidecar); err == nil {
			key, err := base64.StdEncoding.DecodeString(string(data))
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", sidecar, err)
			}
			return key, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read %s: %w", sidecar, err)
		}
		key, err := GenerateMasterKey()
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(sidecar, []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", sidecar, err)
		}
		return key, nil
	}
	return GenerateMasterKey()
}
