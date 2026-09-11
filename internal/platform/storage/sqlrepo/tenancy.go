// Package sqlrepo implements domain repositories with portable database/sql queries.
package sqlrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/F31/liteAIG/internal/tenancy"
)

type queryer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// TenancyRepository implements the tenant-scoped repository contract.
type TenancyRepository struct {
	db *sql.DB
	q  queryer
}

func NewTenancyRepository(db *sql.DB) *TenancyRepository {
	return &TenancyRepository{db: db, q: db}
}

func (r *TenancyRepository) GetTenant(
	ctx context.Context,
	scope tenancy.TenantScope,
) (*tenancy.Tenant, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	tenant, err := scanTenant(r.q.QueryRowContext(ctx, `
SELECT id, public_ref, name, status, settlement_currency, default_project_id, created_at
FROM tenants
WHERE id = $1 AND deleted_at IS NULL`, scope.TenantID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tenancy.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant: %w", err)
	}
	return tenant, nil
}

// FirstTenant returns the earliest created tenant (bootstrap reads).
func (r *TenancyRepository) FirstTenant(ctx context.Context) (*tenancy.Tenant, error) {
	tenant, err := scanTenant(r.q.QueryRowContext(ctx, `
SELECT id, public_ref, name, status, settlement_currency, default_project_id, created_at
FROM tenants
WHERE deleted_at IS NULL
ORDER BY created_at LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tenancy.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("first tenant: %w", err)
	}
	return tenant, nil
}

func (r *TenancyRepository) ListTenants(ctx context.Context, page tenancy.PageRequest) (tenancy.TenantPage, error) {
	if err := page.Validate(); err != nil {
		return tenancy.TenantPage{}, err
	}
	rows, err := r.q.QueryContext(ctx, `
SELECT id, public_ref, name, status, settlement_currency, default_project_id, created_at
FROM tenants
WHERE deleted_at IS NULL
ORDER BY created_at, id
LIMIT $1 OFFSET $2`, page.Limit+1, page.Offset)
	if err != nil {
		return tenancy.TenantPage{}, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()

	items := make([]tenancy.Tenant, 0, page.Limit)
	for rows.Next() {
		tenant, err := scanTenant(rows)
		if err != nil {
			return tenancy.TenantPage{}, fmt.Errorf("scan tenant: %w", err)
		}
		items = append(items, *tenant)
	}
	if err := rows.Err(); err != nil {
		return tenancy.TenantPage{}, fmt.Errorf("list tenants: %w", err)
	}

	var next *int
	if len(items) > page.Limit {
		items = items[:page.Limit]
		value := page.Offset + page.Limit
		next = &value
	}
	return tenancy.TenantPage{Items: items, NextOffset: next}, nil
}

func (r *TenancyRepository) CreateTenantWithDefaultProject(ctx context.Context, tenant tenancy.Tenant, project tenancy.Project) error {
	if r.db == nil {
		return errors.New("create tenant requires root repository")
	}
	if tenant.ID == "" || tenant.PublicRef == "" || tenant.Name == "" || project.ID == "" || project.Name == "" || project.TenantID != tenant.ID {
		return errors.New("tenant and default project are required")
	}
	if tenant.Status == "" {
		tenant.Status = "active"
	}
	if tenant.SettlementCurrency == "" {
		tenant.SettlementCurrency = "USD"
	}
	if project.Status == "" {
		project.Status = "active"
	}
	if project.ResidencyEnforcement == "" {
		project.ResidencyEnforcement = "advisory"
	}
	if tenant.CreatedAt.IsZero() {
		tenant.CreatedAt = time.Now()
	}
	if project.CreatedAt.IsZero() {
		project.CreatedAt = tenant.CreatedAt
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create tenant: %w", err)
	}
	defer tx.Rollback()
	txRepo := &TenancyRepository{q: tx}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO tenants(id, public_ref, name, status, settlement_currency, default_project_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)`, tenant.ID, tenant.PublicRef, tenant.Name, tenant.Status, tenant.SettlementCurrency, project.ID, tenant.CreatedAt); err != nil {
		return fmt.Errorf("create tenant: %w", err)
	}
	if err := txRepo.CreateProject(ctx, tenancy.TenantScope{TenantID: tenant.ID}, project); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit create tenant: %w", err)
	}
	return nil
}

func (r *TenancyRepository) UpdateTenantStatus(ctx context.Context, tenantID, status string) error {
	if tenantID == "" || (status != "active" && status != "suspended" && status != "deleted") {
		return errors.New("tenant id and supported status are required")
	}
	result, err := r.q.ExecContext(ctx, `
UPDATE tenants
SET status = $2,
    suspended_at = CASE WHEN $2 = 'suspended' THEN CURRENT_TIMESTAMP WHEN $2 = 'active' THEN NULL ELSE suspended_at END,
    deleted_at = CASE WHEN $2 = 'deleted' THEN CURRENT_TIMESTAMP ELSE deleted_at END
WHERE id = $1 AND deleted_at IS NULL`, tenantID, status)
	if err != nil {
		return fmt.Errorf("update tenant status: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update tenant status: %w", err)
	}
	if affected == 0 {
		return tenancy.ErrNotFound
	}
	return nil
}

var _ tenancy.TenantLister = (*TenancyRepository)(nil)

func (r *TenancyRepository) CreateProject(
	ctx context.Context,
	scope tenancy.TenantScope,
	project tenancy.Project,
) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if project.TenantID != scope.TenantID {
		return tenancy.ErrScopeMismatch
	}
	regions, err := json.Marshal(project.AllowedDataRegions)
	if err != nil {
		return fmt.Errorf("encode project regions: %w", err)
	}
	_, err = r.q.ExecContext(ctx, `
INSERT INTO projects(
    id, tenant_id, name, allowed_data_regions, residency_enforcement, status, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		project.ID,
		project.TenantID,
		project.Name,
		string(regions),
		project.ResidencyEnforcement,
		project.Status,
		project.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create project: %w", err)
	}
	return nil
}

func (r *TenancyRepository) GetProject(
	ctx context.Context,
	scope tenancy.TenantScope,
	id string,
) (*tenancy.Project, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	project, err := scanProject(r.q.QueryRowContext(ctx, `
SELECT id, tenant_id, name, status, residency_enforcement, allowed_data_regions, created_at
FROM projects
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`, scope.TenantID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tenancy.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	return project, nil
}

func (r *TenancyRepository) ListProjects(
	ctx context.Context,
	scope tenancy.TenantScope,
	page tenancy.PageRequest,
) (tenancy.ProjectPage, error) {
	if err := scope.Validate(); err != nil {
		return tenancy.ProjectPage{}, err
	}
	if err := page.Validate(); err != nil {
		return tenancy.ProjectPage{}, err
	}
	rows, err := r.q.QueryContext(ctx, `
SELECT id, tenant_id, name, status, residency_enforcement, allowed_data_regions, created_at
FROM projects
WHERE tenant_id = $1 AND deleted_at IS NULL
ORDER BY created_at, id
LIMIT $2 OFFSET $3`, scope.TenantID, page.Limit+1, page.Offset)
	if err != nil {
		return tenancy.ProjectPage{}, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	items := make([]tenancy.Project, 0, page.Limit)
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return tenancy.ProjectPage{}, fmt.Errorf("scan project: %w", err)
		}
		items = append(items, *project)
	}
	if err := rows.Err(); err != nil {
		return tenancy.ProjectPage{}, fmt.Errorf("list projects: %w", err)
	}

	var next *int
	if len(items) > page.Limit {
		items = items[:page.Limit]
		value := page.Offset + page.Limit
		next = &value
	}
	return tenancy.ProjectPage{Items: items, NextOffset: next}, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanTenant(row rowScanner) (*tenancy.Tenant, error) {
	var tenant tenancy.Tenant
	var defaultProject sql.NullString
	var createdAt databaseTime
	if err := row.Scan(
		&tenant.ID,
		&tenant.PublicRef,
		&tenant.Name,
		&tenant.Status,
		&tenant.SettlementCurrency,
		&defaultProject,
		&createdAt,
	); err != nil {
		return nil, err
	}
	tenant.DefaultProjectID = defaultProject.String
	tenant.CreatedAt = createdAt.Time
	return &tenant, nil
}

func scanProject(row rowScanner) (*tenancy.Project, error) {
	var project tenancy.Project
	var regionsJSON sql.NullString
	var createdAt databaseTime
	if err := row.Scan(
		&project.ID,
		&project.TenantID,
		&project.Name,
		&project.Status,
		&project.ResidencyEnforcement,
		&regionsJSON,
		&createdAt,
	); err != nil {
		return nil, err
	}
	project.CreatedAt = createdAt.Time
	if regionsJSON.Valid && regionsJSON.String != "" {
		if err := json.Unmarshal([]byte(regionsJSON.String), &project.AllowedDataRegions); err != nil {
			return nil, fmt.Errorf("decode project regions: %w", err)
		}
	}
	return &project, nil
}

func (r *TenancyRepository) WithinTransaction(
	ctx context.Context,
	fn func(tenancy.Repository) error,
) error {
	if r.db == nil {
		return errors.New("nested transaction is not supported")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tenancy transaction: %w", err)
	}
	defer tx.Rollback()

	txRepo := &TenancyRepository{q: tx}
	if err := fn(txRepo); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tenancy transaction: %w", err)
	}
	return nil
}

// Ensure the adapter remains compatible with the domain contracts.
var _ tenancy.Repository = (*TenancyRepository)(nil)
var _ tenancy.Transactor = (*TenancyRepository)(nil)
var _ tenancy.TenantAdmin = (*TenancyRepository)(nil)

type databaseTime struct{ time.Time }

func (t *databaseTime) Scan(value any) error {
	switch value := value.(type) {
	case time.Time:
		t.Time = value
		return nil
	case string:
		return t.parse(value)
	case []byte:
		return t.parse(string(value))
	default:
		return fmt.Errorf("unsupported database time type %T", value)
	}
}

type nullableDatabaseTime struct {
	time.Time
	Valid bool
}

func (t *nullableDatabaseTime) Scan(value any) error {
	if value == nil {
		t.Valid = false
		return nil
	}
	var parsed databaseTime
	if err := parsed.Scan(value); err != nil {
		return err
	}
	t.Time, t.Valid = parsed.Time, true
	return nil
}

func (t *databaseTime) parse(value string) error {
	if index := strings.Index(value, " m="); index >= 0 {
		value = value[:index]
	}
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			t.Time = parsed
			return nil
		}
	}
	return fmt.Errorf("unsupported database time %q", value)
}
