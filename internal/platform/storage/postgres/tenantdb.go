package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/F31/liteAIG/internal/tenancy"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

// TenantRouter is a DBTX that selects a tenant-scoped Postgres pool based on
// the tenant attached to the context. Platform operations (no tenant in
// context) use the trusted pool, which bypasses RLS as the table owner.
//
// Every tenant pool connects as RoleApp and sets app.tenant_id during
// AfterConnect, so RLS applies to every statement without a per-query SET.
type TenantRouter struct {
	platform *sql.DB
	base     *pgx.ConnConfig
	maxPools int

	mu      sync.Mutex
	tenants map[string]*tenantPool
	closed  bool
}

type tenantPool struct {
	db       *sql.DB
	lastUsed time.Time
}

// NewTenantRouter parses the Postgres DSN once and pre-creates nothing; pools
// are opened lazily per tenant and evicted least-recently-used at maxPools.
func NewTenantRouter(dsn string, platform *sql.DB, maxPools int) (*TenantRouter, error) {
	if platform == nil {
		return nil, fmt.Errorf("tenant router requires a platform pool")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres DSN for tenant router: %w", err)
	}
	if maxPools <= 0 {
		maxPools = 16
	}
	return &TenantRouter{platform: platform, base: config, maxPools: maxPools, tenants: map[string]*tenantPool{}}, nil
}

func (r *TenantRouter) pool(ctx context.Context) (*sql.DB, error) {
	tenantID := tenancy.TenantFromContext(ctx)
	if tenantID == "" {
		return r.platform, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, fmt.Errorf("tenant router is closed")
	}
	if existing := r.tenants[tenantID]; existing != nil {
		existing.lastUsed = time.Now()
		return existing.db, nil
	}
	for len(r.tenants) >= r.maxPools {
		oldestID := ""
		var oldest time.Time
		for id, pool := range r.tenants {
			if oldestID == "" || pool.lastUsed.Before(oldest) {
				oldestID, oldest = id, pool.lastUsed
			}
		}
		if oldestID == "" {
			break
		}
		_ = r.tenants[oldestID].db.Close()
		delete(r.tenants, oldestID)
	}
	config := *r.base
	config.AfterConnect = func(ctx context.Context, conn *pgconn.PgConn) error {
		if _, err := conn.Exec(ctx, "SET ROLE "+RoleApp).ReadAll(); err != nil {
			return fmt.Errorf("set tenant role: %w", err)
		}
		statement := "SET app.tenant_id = " + quoteLiteral(tenantID)
		if _, err := conn.Exec(ctx, statement).ReadAll(); err != nil {
			return fmt.Errorf("set tenant context: %w", err)
		}
		return nil
	}
	db := stdlib.OpenDB(config)
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open tenant pool %s: %w", tenantID, err)
	}
	r.tenants[tenantID] = &tenantPool{db: db, lastUsed: time.Now()}
	return db, nil
}

// ExecContext routes the statement to the context tenant's pool.
func (r *TenantRouter) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	db, err := r.pool(ctx)
	if err != nil {
		return nil, err
	}
	return db.ExecContext(ctx, query, args...)
}

// QueryContext routes the query to the context tenant's pool.
func (r *TenantRouter) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	db, err := r.pool(ctx)
	if err != nil {
		return nil, err
	}
	return db.QueryContext(ctx, query, args...)
}

// QueryRowContext routes the query to the context tenant's pool.
func (r *TenantRouter) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	db, err := r.pool(ctx)
	if err != nil {
		return r.platform.QueryRowContext(ctx, "SELECT NULL WHERE FALSE")
	}
	return db.QueryRowContext(ctx, query, args...)
}

// BeginTx starts a transaction on the context tenant's pool.
func (r *TenantRouter) BeginTx(ctx context.Context, options *sql.TxOptions) (*sql.Tx, error) {
	db, err := r.pool(ctx)
	if err != nil {
		return nil, err
	}
	return db.BeginTx(ctx, options)
}

// Close closes every tenant pool. The platform pool is owned by the caller.
func (r *TenantRouter) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	var err error
	for id, pool := range r.tenants {
		if closeErr := pool.db.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		delete(r.tenants, id)
	}
	return err
}

// quoteRoleIdent quotes a Postgres role/identifier for GRANT statements.
func quoteRoleIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
