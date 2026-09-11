// Package postgres provides the Standard PostgreSQL storage adapter.
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	controlconfig "github.com/F31/liteAIG/internal/controlplane/config"
	"github.com/F31/liteAIG/internal/controlplane/setup"
	"github.com/F31/liteAIG/internal/finops/accounting"
	"github.com/F31/liteAIG/internal/finops/budget"
	"github.com/F31/liteAIG/internal/identity"
	"github.com/F31/liteAIG/internal/identity/apikey"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/observability/alert"
	"github.com/F31/liteAIG/internal/organization"
	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/internal/tenancy"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Open creates a bounded PostgreSQL pool: at most 32 live connections, 8
// kept idle, and a 30-minute connection lifetime so long-lived servers do not
// hold stale sessions past server-side restarts or failovers.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("postgres DSN is required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	db.SetMaxOpenConns(32)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
	return db, nil
}

func NewTenancyRepository(db *sql.DB) tenancy.Repository {
	return sqlrepo.NewTenancyRepository(db)
}

func NewTenancyTransactor(db *sql.DB) tenancy.Transactor {
	return sqlrepo.NewTenancyRepository(db)
}

func NewTenancyAdmin(db *sql.DB) tenancy.TenantAdmin {
	return sqlrepo.NewTenancyRepository(db)
}

func NewBootstrapRepository(db *sql.DB) setup.Repository {
	return sqlrepo.NewBootstrapRepository(db)
}

func NewConfigRepository(db *sql.DB) controlconfig.Repository {
	return sqlrepo.NewConfigRepository(db)
}

func NewAPIKeyRepository(db *sql.DB) apikey.Repository { return sqlrepo.NewAPIKeyRepository(db) }
func NewAccountingRepository(db *sql.DB) accounting.Repository {
	return sqlrepo.NewAccountingRepository(db)
}
func NewLocalCredentialStore(db *sql.DB) identity.LocalCredentialStore {
	return sqlrepo.NewLocalCredentialStore(db)
}
func NewBudgetAuditRepository(db *sql.DB) budget.AuditRepository {
	return sqlrepo.NewBudgetAuditRepository(db)
}
func NewOrganizationRepository(db *sql.DB) organization.Repository {
	return sqlrepo.NewOrganizationRepository(db)
}
func NewAlertStore(db *sql.DB) alert.Store {
	return sqlrepo.NewAlertStore(db)
}
func NewAlertAuditRecorder(db *sql.DB, ids contracts.IDGenerator) alert.Auditor {
	return sqlrepo.NewAlertAuditRecorder(db, ids)
}
