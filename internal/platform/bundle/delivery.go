// Wire serialization and signing for signed RuntimeBundle delivery between the
// Control Plane and Data Planes (Stage 11). Bundles travel as JSON carrying the
// serializable TenantSnapshotData; the compiled snapshot is rebuilt locally.
package bundle

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/F31/liteAIG/internal/kernel/runtime"
)

// wireBundle is the JSON form of a RuntimeBundle: the snapshot is carried as
// its serializable data record rather than the compiled in-memory indexes.
type wireBundle struct {
	SchemaVersion        string
	TenantID, TenantRef  string
	ConfigVersion        int64
	SecurityEpoch        int64
	SystemRuntimeVersion int64
	PublishedAt          time.Time
	PayloadChecksum      string
	Signature            []byte
	SnapshotData         runtime.TenantSnapshotData
}

// Encode serializes the bundle for wire delivery, rebuilding the snapshot from
// its serializable data so the compiled indexes never leak into the payload.
func (b *RuntimeBundle) Encode() ([]byte, error) {
	if b == nil || b.Snapshot == nil {
		return nil, errors.New("bundle has no snapshot to encode")
	}
	return json.Marshal(wireBundle{
		SchemaVersion: b.SchemaVersion, TenantID: b.TenantID, TenantRef: b.TenantRef,
		ConfigVersion: b.ConfigVersion, SecurityEpoch: b.SecurityEpoch,
		SystemRuntimeVersion: b.SystemRuntimeVersion, PublishedAt: b.PublishedAt,
		PayloadChecksum: b.PayloadChecksum, Signature: b.Signature,
		SnapshotData: b.Snapshot.Data(),
	})
}

// Decode parses a wire bundle and rebuilds the compiled snapshot. The caller
// must still run Prepare (signature/checksum/schema) before activation.
func Decode(data []byte) (*RuntimeBundle, error) {
	var wire wireBundle
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, err
	}
	if wire.SnapshotData.TenantRef == "" || wire.ConfigVersion == 0 {
		return nil, errors.New("incomplete wire bundle")
	}
	return &RuntimeBundle{
		SchemaVersion: wire.SchemaVersion, TenantID: wire.TenantID, TenantRef: wire.TenantRef,
		ConfigVersion: wire.ConfigVersion, SecurityEpoch: wire.SecurityEpoch,
		SystemRuntimeVersion: wire.SystemRuntimeVersion, PublishedAt: wire.PublishedAt,
		PayloadChecksum: wire.PayloadChecksum, Signature: wire.Signature,
		Snapshot: runtime.NewTenantSnapshot(wire.SnapshotData),
	}, nil
}

// Signer produces signed, immutable bundles from compiled snapshots. It is held
// by the Control Plane; the matching public key is shipped to Data Planes.
type Signer struct {
	privateKey    ed25519.PrivateKey
	schema        string
	systemVersion int64
}

// NewSigner builds a Control-Plane bundle signer.
func NewSigner(privateKey ed25519.PrivateKey, schema string, systemVersion int64) *Signer {
	return &Signer{privateKey: privateKey, schema: schema, systemVersion: systemVersion}
}

// PublicKey returns the verification key Data Planes must trust.
func (s *Signer) PublicKey() ed25519.PublicKey {
	if s.privateKey == nil {
		return nil
	}
	public, ok := s.privateKey.Public().(ed25519.PublicKey)
	if !ok {
		return nil
	}
	return public
}

// PublicKeyHex returns the hex form of the verification key for wiring Data
// Planes (e.g. as a CLI flag or runtime config).
func (s *Signer) PublicKeyHex() string {
	return hex.EncodeToString(s.PublicKey())
}

// Sign wraps a compiled snapshot in a fresh RuntimeBundle, signs it, and
// returns wire bytes ready for delivery. The bundle is immutable once signed.
func (s *Signer) Sign(snapshot *runtime.TenantRuntimeSnapshot) (*RuntimeBundle, error) {
	schema := s.schema
	if schema == "" {
		schema = "v1"
	}
	bundle := &RuntimeBundle{
		SchemaVersion:        schema,
		TenantID:             snapshot.TenantID,
		TenantRef:            snapshot.TenantRef,
		ConfigVersion:        snapshot.Version,
		SecurityEpoch:        snapshot.SecurityEpoch,
		SystemRuntimeVersion: s.systemVersion,
		PublishedAt:          snapshot.PublishedAt,
		Snapshot:             snapshot,
	}
	if err := bundle.Sign(s.privateKey); err != nil {
		return nil, err
	}
	return bundle, nil
}

// VerifyPublicKey returns the canonical 32-byte form of a hex public key.
func VerifyPublicKey(hexKey string) (ed25519.PublicKey, error) {
	if hexKey == "" {
		return nil, errors.New("bundle public key is required")
	}
	decoded, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, err
	}
	if len(decoded) != ed25519.PublicKeySize {
		return nil, errors.New("invalid bundle public key length")
	}
	return ed25519.PublicKey(decoded), nil
}

// NewSignerFromHex builds a Control-Plane signer from a hex-encoded Ed25519
// private key (64-byte seed+public concatenation).
func NewSignerFromHex(privateKeyHex, schema string, systemVersion int64) (*Signer, error) {
	if privateKeyHex == "" {
		return nil, errors.New("bundle signing key is required")
	}
	decoded, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return nil, err
	}
	if len(decoded) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid bundle signing key length")
	}
	return NewSigner(ed25519.PrivateKey(decoded), schema, systemVersion), nil
}
