// Package bundle owns the signed immutable RuntimeBundle delivered to the Data
// Plane: metadata, payload checksum, Ed25519 signature, and the compiled
// snapshot. Bundles never embed plaintext secrets.
package bundle

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/F31/liteAIG/internal/kernel/runtime"
)

// RuntimeBundle is the immutable, signed unit of configuration delivery.
type RuntimeBundle struct {
	SchemaVersion        string
	TenantID             string
	TenantRef            string
	ConfigVersion        int64
	SecurityEpoch        int64
	SystemRuntimeVersion int64
	PublishedAt          time.Time

	PayloadChecksum string
	Signature       []byte
	Snapshot        *runtime.TenantRuntimeSnapshot
}

// Payload is the canonical, checksummed serialization of a bundle's metadata and
// snapshot. The signature covers this payload.
func (b *RuntimeBundle) Payload() ([]byte, error) {
	type wire struct {
		SchemaVersion        string
		TenantID, TenantRef  string
		ConfigVersion        int64
		SecurityEpoch        int64
		SystemRuntimeVersion int64
		PublishedAt          time.Time
		Snapshot             *runtime.TenantRuntimeSnapshot
	}
	payload, err := json.Marshal(wire{
		SchemaVersion: b.SchemaVersion, TenantID: b.TenantID, TenantRef: b.TenantRef,
		ConfigVersion: b.ConfigVersion, SecurityEpoch: b.SecurityEpoch,
		SystemRuntimeVersion: b.SystemRuntimeVersion, PublishedAt: b.PublishedAt,
		Snapshot: b.Snapshot,
	})
	if err != nil {
		return nil, err
	}
	return payload, nil
}

// Checksum returns the SHA-256 hex of the canonical payload.
func (b *RuntimeBundle) Checksum() (string, error) {
	payload, err := b.Payload()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

// Sign signs the canonical payload with the private key and sets both the
// checksum and signature.
func (b *RuntimeBundle) Sign(privateKey ed25519.PrivateKey) error {
	checksum, err := b.Checksum()
	if err != nil {
		return err
	}
	b.PayloadChecksum = checksum
	signature := ed25519.Sign(privateKey, []byte(checksum))
	b.Signature = signature
	return nil
}

// Verify checks the signature over the payload checksum with the public key and
// verifies the stored checksum matches the current payload.
func (b *RuntimeBundle) Verify(publicKey ed25519.PublicKey) error {
	if len(b.Signature) == 0 {
		return errors.New("bundle is not signed")
	}
	checksum, err := b.Checksum()
	if err != nil {
		return err
	}
	if checksum != b.PayloadChecksum {
		return errors.New("bundle payload checksum mismatch")
	}
	if !ed25519.Verify(publicKey, []byte(checksum), b.Signature) {
		return errors.New("bundle signature verification failed")
	}
	return nil
}

// GenerateKey returns a fresh Ed25519 signing key pair (tests/default).
func GenerateKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

// EqualPayload reports whether two bundles share the same canonical payload.
func EqualPayload(a, b *RuntimeBundle) bool {
	pa, _ := a.Payload()
	pb, _ := b.Payload()
	return bytes.Equal(pa, pb)
}
