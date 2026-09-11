// Package lkg persists the Data Plane's Last Known Good RuntimeBundles on local
// disk (active.bundle / previous.bundle per tenant) with temp+fsync+atomic
// rename, and provides fallback boot when the Control Plane is unreachable.
package lkg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/F31/liteAIG/internal/kernel/runtime"
)

// Bundle is the serializable bundle record persisted on disk.
type Bundle struct {
	SchemaVersion   string                      `json:"schemaVersion"`
	TenantID        string                      `json:"tenantId"`
	TenantRef       string                      `json:"tenantRef"`
	ConfigVersion   int64                       `json:"configVersion"`
	SecurityEpoch   int64                       `json:"securityEpoch"`
	PayloadChecksum string                      `json:"payloadChecksum"`
	Signature       []byte                      `json:"signature"`
	SnapshotData    *runtime.TenantSnapshotData `json:"snapshotData"`
}

// Store persists active/previous bundles per tenant with atomic writes.
type Store struct {
	dir string
}

// Verifier validates a persisted bundle before it is accepted for boot.
type Verifier func(*Bundle) error

// New creates an LKG store rooted at dir.
func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("LKG directory is required")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) path(tenantRef, name string) string {
	return filepath.Join(s.dir, tenantRef, name)
}

// Save writes the bundle as active.bundle atomically (temp+fsync+rename) and
// rotates the prior active to previous.bundle.
func (s *Store) Save(_ context.Context, tenantRef string, bundle Bundle) error {
	data, err := json.Marshal(bundle)
	if err != nil {
		return err
	}
	tenantDir := filepath.Join(s.dir, tenantRef)
	if err := os.MkdirAll(tenantDir, 0o750); err != nil {
		return err
	}
	activePath := s.path(tenantRef, "active.bundle")
	previousPath := s.path(tenantRef, "previous.bundle")
	if _, err := os.Stat(activePath); err == nil {
		_ = os.Rename(activePath, previousPath)
	}
	return writeAtomic(activePath, data)
}

// writeAtomic writes data to a temp file, fsyncs, then atomically renames it.
func writeAtomic(path string, data []byte) error {
	temp := path + ".tmp"
	file, err := os.OpenFile(temp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

// LoadActive returns the active bundle, falling back to previous.bundle when
// active is corrupt.
func (s *Store) LoadActive(_ context.Context, tenantRef string) (*Bundle, error) {
	bundle, err := s.read(s.path(tenantRef, "active.bundle"))
	if err == nil {
		return bundle, nil
	}
	previous, previousErr := s.read(s.path(tenantRef, "previous.bundle"))
	if previousErr == nil {
		return previous, nil
	}
	return nil, fmt.Errorf("active and previous bundles unreadable: %w", err)
}

// LoadVerified returns the first active/previous bundle accepted by verifier.
// This ensures a structurally readable but invalid active bundle cannot prevent
// a valid previous bundle from booting.
func (s *Store) LoadVerified(_ context.Context, tenantRef string, verifier Verifier) (*Bundle, error) {
	if verifier == nil {
		return nil, errors.New("LKG bundle verifier is required")
	}
	var activeErr error
	for _, name := range []string{"active.bundle", "previous.bundle"} {
		candidate, err := s.read(s.path(tenantRef, name))
		if err == nil {
			err = verifier(candidate)
		}
		if err == nil {
			return candidate, nil
		}
		if activeErr == nil {
			activeErr = err
		}
	}
	return nil, fmt.Errorf("active and previous bundles failed verification: %w", activeErr)
}

func (s *Store) read(path string) (*Bundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var bundle Bundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return nil, err
	}
	if bundle.TenantRef == "" || bundle.ConfigVersion == 0 {
		return nil, errors.New("invalid LKG bundle")
	}
	return &bundle, nil
}

// Ready reports whether a tenant has a loadable active (or previous) bundle.
// Data Plane readiness for a tenant requires this, coupling readiness to bundle
// validity.
func (s *Store) Ready(ctx context.Context, tenantRef string) bool {
	_, err := s.LoadActive(ctx, tenantRef)
	return err == nil
}

// TenantRefs lists the tenant refs that have at least one persisted bundle, so
// boot can fall back to a Last Known Good bundle for a tenant whose
// Control-Plane state is missing or unreadable.
func (s *Store) TenantRefs() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var refs []string
	for _, entry := range entries {
		if entry.IsDir() {
			refs = append(refs, entry.Name())
		}
	}
	return refs, nil
}
