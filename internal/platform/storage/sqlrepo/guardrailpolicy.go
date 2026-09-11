package sqlrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	guardraildomain "github.com/F31/liteAIG/internal/guardrail"
	"github.com/F31/liteAIG/internal/tenancy"
)

// GuardrailPolicyStore persists tenant-scoped guardrail policies.
type GuardrailPolicyStore struct{ db *sql.DB }

func NewGuardrailPolicyStore(db *sql.DB) *GuardrailPolicyStore { return &GuardrailPolicyStore{db: db} }

func (s *GuardrailPolicyStore) Create(ctx context.Context, scope tenancy.TenantScope, policy guardraildomain.Policy, change guardraildomain.ChangeType, actor string) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if policy.TenantID != scope.TenantID {
		return tenancy.ErrScopeMismatch
	}
	rules, err := json.Marshal(policy.Rules)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO guardrail_policies(policy_id,tenant_id,version,security_epoch,change_type,rules,actor,published_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		policy.ID, policy.TenantID, policy.Version, policy.SecurityEpoch, string(change), string(rules), actor, policy.PublishedAt)
	return err
}

func (s *GuardrailPolicyStore) GetActive(ctx context.Context, scope tenancy.TenantScope) (*guardraildomain.Policy, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	var policy guardraildomain.Policy
	var rules string
	var changeType string
	err := s.db.QueryRowContext(ctx, `SELECT policy_id,tenant_id,version,security_epoch,change_type,rules FROM guardrail_policies WHERE tenant_id=$1 ORDER BY version DESC LIMIT 1`, scope.TenantID).Scan(&policy.ID, &policy.TenantID, &policy.Version, &policy.SecurityEpoch, &changeType, &rules)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tenancy.ErrNotFound
	}
	policy.ChangeType = guardraildomain.ChangeType(changeType)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(rules), &policy.Rules); err != nil {
		return nil, err
	}
	return &policy, nil
}

var _ guardraildomain.PolicyStore = (*GuardrailPolicyStore)(nil)
