package sqlrepo

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"time"

	"github.com/F31/liteAIG/internal/platform/coordination"
)

// CoordinationLeaseStore implements coordination.LeaseCoordinator over the
// platform-owned coordination_leases table. Because the table is keyed by a
// single row per scope (PK on scope), it provides the leader-election
// semantic §2.2 requires for singleton tasks: at most one process holds a
// scope at a time, and a crashed process is handed back the lease once its
// expiry passes.
type CoordinationLeaseStore struct {
	db    *sql.DB
	clock func() time.Time
}

func NewCoordinationLeaseStore(db *sql.DB, clock func() time.Time) *CoordinationLeaseStore {
	if clock == nil {
		clock = time.Now
	}
	return &CoordinationLeaseStore{db: db, clock: clock}
}

// Acquire takes the scope lease when it is free or expired. The database row
// is the exclusive producer of truth, so two processes racing on Acquire see a
// serialized result; the loser gets (false, "", nil) as a busy signal. The
// capacity argument must be 1 (leader election); any other value is treated as
// busy because a single-row table cannot express shared capacity.
func (s *CoordinationLeaseStore) Acquire(ctx context.Context, scope string, capacity int, ttl time.Duration) (bool, string, error) {
	if scope == "" || ttl <= 0 {
		return false, "", nil
	}
	if capacity != 1 {
		return false, "", nil
	}
	leaseID, err := coordinationLeaseID()
	if err != nil {
		return false, "", err
	}
	now := s.clock().UTC()
	expiresAt := now.Add(ttl)
	result, err := s.db.ExecContext(ctx, `
INSERT INTO coordination_leases(scope, lease_id, owner, expires_at, created_at, updated_at)
VALUES ($1, $2, 'platform', $3, $4, $4)
ON CONFLICT(scope) DO UPDATE SET
    lease_id = EXCLUDED.lease_id,
    owner = 'platform',
    expires_at = EXCLUDED.expires_at,
    updated_at = EXCLUDED.updated_at
WHERE coordination_leases.expires_at < $4`,
		scope, leaseID, expiresAt, now)
	if err != nil {
		return false, "", err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, "", err
	}
	// When the conflict clause's guard rejects the update, RowsAffected is 0:
	// the lease is still owned by a live holder.
	return affected != 0, leaseID, nil
}

// Renew extends the lease only while the caller still owns it. A renew on a
// lost or expired lease returns (false, nil).
func (s *CoordinationLeaseStore) Renew(ctx context.Context, scope, leaseID string, ttl time.Duration) (bool, error) {
	if scope == "" || leaseID == "" || ttl <= 0 {
		return false, nil
	}
	now := s.clock().UTC()
	expiresAt := now.Add(ttl)
	result, err := s.db.ExecContext(ctx, `
UPDATE coordination_leases
SET expires_at = $3, updated_at = $4, owner = 'platform'
WHERE scope = $1 AND lease_id = $2 AND expires_at > $4`,
		scope, leaseID, expiresAt, now)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected != 0, nil
}

// Release drops the leader's ownership. A release by a non-owner (leadership
// already lost) is a no-op but never an error, matching the coordination
// contract.
func (s *CoordinationLeaseStore) Release(ctx context.Context, scope, leaseID string) error {
	if scope == "" || leaseID == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM coordination_leases WHERE scope = $1 AND lease_id = $2`, scope, leaseID)
	return err
}

// Cleanup removes every expired lease for the scope.
func (s *CoordinationLeaseStore) Cleanup(ctx context.Context, scope string) error {
	if scope == "" {
		return nil
	}
	now := s.clock().UTC()
	_, err := s.db.ExecContext(ctx, `DELETE FROM coordination_leases WHERE scope = $1 AND expires_at <= $2`, scope, now)
	return err
}

var _ coordination.LeaseCoordinator = (*CoordinationLeaseStore)(nil)

// coordinationLeaseID returns a random opaque lease identifier.
func coordinationLeaseID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
