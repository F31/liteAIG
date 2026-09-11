package sqlrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/F31/liteAIG/internal/federation"
	"github.com/F31/liteAIG/internal/tenancy"
)

// FederationStore persists federated agent trust relationships as JSON bodies
// with indexed lookup columns.
type FederationStore struct{ db *sql.DB }

func NewFederationStore(db *sql.DB) *FederationStore { return &FederationStore{db: db} }

// Put inserts or replaces a relationship.
func (s *FederationStore) Put(ctx context.Context, relationship federation.Relationship) error {
	body, err := json.Marshal(relationship)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO federation_relationships(id, tenant_id, external_agent_id, name, status, body, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT(id) DO UPDATE SET external_agent_id=EXCLUDED.external_agent_id, name=EXCLUDED.name,
  status=EXCLUDED.status, body=EXCLUDED.body, updated_at=EXCLUDED.updated_at`,
		relationship.ID, relationship.TenantID, relationship.ExternalAgentID, relationship.Name,
		string(relationship.Status), string(body), relationship.CreatedAt, relationship.UpdatedAt)
	return err
}

// Get returns the relationship for id within the tenant scope.
func (s *FederationStore) Get(ctx context.Context, scope tenancy.TenantScope, id string) (federation.Relationship, bool) {
	var body []byte
	err := s.db.QueryRowContext(ctx, `SELECT body FROM federation_relationships WHERE id=$1 AND tenant_id=$2`, id, scope.TenantID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return federation.Relationship{}, false
	}
	if err != nil {
		return federation.Relationship{}, false
	}
	var relationship federation.Relationship
	if err := json.Unmarshal(body, &relationship); err != nil {
		return federation.Relationship{}, false
	}
	return relationship, true
}

// List returns every relationship in the tenant scope.
func (s *FederationStore) List(ctx context.Context, scope tenancy.TenantScope) ([]federation.Relationship, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT body FROM federation_relationships WHERE tenant_id=$1 ORDER BY created_at`, scope.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []federation.Relationship
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var relationship federation.Relationship
		if err := json.Unmarshal(body, &relationship); err != nil {
			return nil, err
		}
		result = append(result, relationship)
	}
	return result, rows.Err()
}
