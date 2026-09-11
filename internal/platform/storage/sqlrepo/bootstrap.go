package sqlrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/F31/liteAIG/internal/controlplane/setup"
)

type BootstrapRepository struct {
	db *sql.DB
}

func NewBootstrapRepository(db *sql.DB) *BootstrapRepository {
	return &BootstrapRepository{db: db}
}

func (r *BootstrapRepository) CreateInitial(ctx context.Context, state setup.InitialState) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin setup transaction: %w", err)
	}
	defer tx.Rollback()

	var adminCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM local_admins`).Scan(&adminCount); err != nil {
		return fmt.Errorf("check local admins: %w", err)
	}
	if adminCount > 0 {
		return setup.ErrAlreadyInitialized
	}

	role := state.Admin.Role
	if role == "" {
		role = "tenant_admin"
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO local_admins(id, username, password_hash, role, status, created_at)
VALUES ($1, $2, $3, $4, $5, $6)`,
		state.Admin.ID,
		state.Admin.Username,
		state.Admin.PasswordHash,
		role,
		state.Admin.Status,
		state.Admin.CreatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return setup.ErrAlreadyInitialized
		}
		return fmt.Errorf("create local admin: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO tenants(
    id, public_ref, name, status, settlement_currency, default_project_id, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		state.Tenant.ID,
		state.Tenant.PublicRef,
		state.Tenant.Name,
		state.Tenant.Status,
		state.Tenant.SettlementCurrency,
		state.Tenant.DefaultProjectID,
		state.Tenant.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create initial tenant: %w", err)
	}

	regions, err := json.Marshal(state.Project.AllowedDataRegions)
	if err != nil {
		return fmt.Errorf("encode default project regions: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO projects(
    id, tenant_id, name, allowed_data_regions, residency_enforcement, status, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		state.Project.ID,
		state.Project.TenantID,
		state.Project.Name,
		string(regions),
		state.Project.ResidencyEnforcement,
		state.Project.Status,
		state.Project.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create default project: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit setup transaction: %w", err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	type sqlState interface{ SQLState() string }
	var state sqlState
	if errors.As(err, &state) && state.SQLState() == "23505" {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}

var _ setup.Repository = (*BootstrapRepository)(nil)
