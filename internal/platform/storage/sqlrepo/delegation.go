package sqlrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/F31/liteAIG/internal/identity"
	"github.com/F31/liteAIG/internal/tenancy"
)

// DelegationStore persists identity-owned agent delegation grants.
type DelegationStore struct{ db *sql.DB }

func NewDelegationStore(db *sql.DB) *DelegationStore { return &DelegationStore{db: db} }

func (s *DelegationStore) List(ctx context.Context, scope tenancy.TenantScope) ([]identity.DelegationGrant, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, tenant_id, delegator_id, delegatee_id, permissions_json, created_by, created_at
FROM delegation_grants WHERE tenant_id=$1 ORDER BY created_at, id`, scope.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []identity.DelegationGrant
	for rows.Next() {
		grant, err := scanDelegation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, grant)
	}
	return result, rows.Err()
}

func (s *DelegationStore) Put(ctx context.Context, scope tenancy.TenantScope, grant identity.DelegationGrant) (identity.DelegationGrant, error) {
	permissions, err := json.Marshal(grant.Permissions)
	if err != nil {
		return identity.DelegationGrant{}, err
	}
	grant.TenantID = scope.TenantID
	_, err = s.db.ExecContext(ctx, `
INSERT INTO delegation_grants(id, tenant_id, delegator_id, delegatee_id, permissions_json, created_by, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT(tenant_id, delegator_id, delegatee_id) DO UPDATE SET
  id=EXCLUDED.id, permissions_json=EXCLUDED.permissions_json, created_by=EXCLUDED.created_by, created_at=EXCLUDED.created_at`,
		grant.ID, grant.TenantID, grant.DelegatorID, grant.DelegateeID, string(permissions), grant.CreatedBy, grant.CreatedAt)
	if err != nil {
		return identity.DelegationGrant{}, err
	}
	return grant, nil
}

func (s *DelegationStore) Delete(ctx context.Context, scope tenancy.TenantScope, id string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM delegation_grants WHERE tenant_id=$1 AND id=$2`, scope.TenantID, id)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

type delegationScanner interface {
	Scan(dest ...any) error
}

func scanDelegation(row delegationScanner) (identity.DelegationGrant, error) {
	var grant identity.DelegationGrant
	var permissions string
	var createdAt databaseTime
	if err := row.Scan(&grant.ID, &grant.TenantID, &grant.DelegatorID, &grant.DelegateeID, &permissions, &grant.CreatedBy, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return identity.DelegationGrant{}, err
		}
		return identity.DelegationGrant{}, err
	}
	if err := json.Unmarshal([]byte(permissions), &grant.Permissions); err != nil {
		return identity.DelegationGrant{}, err
	}
	grant.CreatedAt = createdAt.Time
	return grant, nil
}
