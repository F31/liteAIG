package sqlrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/F31/liteAIG/internal/organization"
	"github.com/F31/liteAIG/internal/tenancy"
)

// OrganizationRepository implements the effective-dated organization contract.
type OrganizationRepository struct{ db *sql.DB }

func NewOrganizationRepository(db *sql.DB) *OrganizationRepository {
	return &OrganizationRepository{db: db}
}

func (r *OrganizationRepository) GetUser(ctx context.Context, scope tenancy.TenantScope, id string) (*organization.User, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	// Users are global identity; tenant membership scopes the lookup.
	var user organization.User
	var createdAt databaseTime
	err := r.db.QueryRowContext(ctx, `
SELECT u.id, u.display_name, u.email, u.status, u.created_at
FROM users u
JOIN tenant_memberships tm ON tm.user_id = u.id AND tm.tenant_id = $1
WHERE u.id = $2`, scope.TenantID, id).Scan(
		&user.ID, &user.DisplayName, &user.Email, &user.Status, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tenancy.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	user.CreatedAt = createdAt.Time
	return &user, nil
}

func (r *OrganizationRepository) GetOrgUnit(ctx context.Context, scope tenancy.TenantScope, id string) (*organization.OrgUnit, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	var unit organization.OrgUnit
	var parent, code, costCenter sql.NullString
	var createdAt databaseTime
	err := r.db.QueryRowContext(ctx, `
SELECT id, tenant_id, parent_id, name, code, cost_center_id, path, status, created_at
FROM org_units
WHERE tenant_id = $1 AND id = $2`, scope.TenantID, id).Scan(
		&unit.ID, &unit.TenantID, &parent, &unit.Name, &code, &costCenter, &unit.Path, &unit.Status, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tenancy.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	unit.ParentID, unit.Code, unit.CostCenterID = parent.String, code.String, costCenter.String
	unit.CreatedAt = createdAt.Time
	return &unit, nil
}

func (r *OrganizationRepository) HasActiveTenantMembership(ctx context.Context, userID, tenantID string) (bool, error) {
	if userID == "" || tenantID == "" {
		return false, nil
	}
	var exists int
	err := r.db.QueryRowContext(ctx, `
SELECT 1
FROM tenant_memberships tm
JOIN tenants t ON t.id = tm.tenant_id AND t.status = 'active'
JOIN users u ON u.id = tm.user_id AND u.status = 'active'
WHERE tm.user_id = $1 AND tm.tenant_id = $2 AND tm.status = 'active'
LIMIT 1`, userID, tenantID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *OrganizationRepository) ActiveAssignment(ctx context.Context, scope tenancy.TenantScope, userID string, at time.Time) (*organization.UserOrgAssignment, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return r.scanAssignment(r.db.QueryRowContext(ctx, `
SELECT a.id, a.tenant_id, a.user_id, a.org_unit_id, a.is_primary, a.valid_from, a.valid_to, a.created_at
FROM user_org_assignments a
WHERE a.tenant_id = $1 AND a.user_id = $2
  AND a.valid_from <= $3 AND (a.valid_to IS NULL OR a.valid_to > $3)
ORDER BY a.is_primary DESC, a.valid_from DESC
LIMIT 1`, scope.TenantID, userID, at))
}

func (r *OrganizationRepository) ListAssignments(ctx context.Context, scope tenancy.TenantScope, userID string) ([]organization.UserOrgAssignment, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT a.id, a.tenant_id, a.user_id, a.org_unit_id, a.is_primary, a.valid_from, a.valid_to, a.created_at
FROM user_org_assignments a
WHERE a.tenant_id = $1 AND a.user_id = $2
ORDER BY a.valid_from`, scope.TenantID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []organization.UserOrgAssignment
	for rows.Next() {
		assignment, err := r.scanAssignment(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *assignment)
	}
	return result, rows.Err()
}

func (r *OrganizationRepository) CreateOrgUnit(ctx context.Context, scope tenancy.TenantScope, unit organization.OrgUnit) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if unit.TenantID != scope.TenantID {
		return tenancy.ErrScopeMismatch
	}
	_, err := r.db.ExecContext(ctx, `
INSERT INTO org_units(id, tenant_id, parent_id, name, code, cost_center_id, path, status, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		unit.ID, unit.TenantID, nullString(unit.ParentID), unit.Name, nullString(unit.Code),
		nullString(unit.CostCenterID), unit.Path, unit.Status, unit.CreatedAt,
	)
	return err
}

func (r *OrganizationRepository) AssignUser(ctx context.Context, scope tenancy.TenantScope, assignment organization.UserOrgAssignment) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if assignment.TenantID != scope.TenantID {
		return tenancy.ErrScopeMismatch
	}
	if assignment.ValidTo != nil && !assignment.ValidTo.After(assignment.ValidFrom) {
		return errors.New("assignment valid_to must be after valid_from")
	}
	_, err := r.db.ExecContext(ctx, `
INSERT INTO user_org_assignments(id, tenant_id, user_id, org_unit_id, is_primary, valid_from, valid_to, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		assignment.ID, assignment.TenantID, assignment.UserID, assignment.OrgUnitID,
		assignment.IsPrimary, assignment.ValidFrom, assignment.ValidTo, assignment.CreatedAt,
	)
	return err
}

func (r *OrganizationRepository) scanAssignment(row rowScanner) (*organization.UserOrgAssignment, error) {
	var assignment organization.UserOrgAssignment
	var validFrom, createdAt databaseTime
	var validTo nullableDatabaseTime
	err := row.Scan(
		&assignment.ID, &assignment.TenantID, &assignment.UserID, &assignment.OrgUnitID,
		&assignment.IsPrimary, &validFrom, &validTo, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tenancy.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan assignment: %w", err)
	}
	assignment.ValidFrom = validFrom.Time
	assignment.CreatedAt = createdAt.Time
	if validTo.Valid {
		value := validTo.Time
		assignment.ValidTo = &value
	}
	return &assignment, nil
}

var _ organization.Repository = (*OrganizationRepository)(nil)
