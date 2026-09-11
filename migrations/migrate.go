// Package migrations embeds and applies the ordered Phase 0 database schema.
package migrations

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed *.sql
var files embed.FS

// Apply executes each migration exactly once in lexical filename order.
func Apply(ctx context.Context, db *sql.DB) error {
	// Postgres-specific advisory lock serializes concurrent replicas that all
	// start in mode=all. Without it, two pods creating schema_migrations at the
	// same instant race the pg_type catalog (SQLSTATE 23505) and crash.
	lock, err := acquireApplyLock(ctx, db)
	if err != nil {
		return err
	}
	defer lock.release()

	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.Glob(files, "*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(entries)
	for _, name := range entries {
		if err := applyOne(ctx, db, name); err != nil {
			return err
		}
	}
	return nil
}

// applyLock is a dedicated session (or a no-op) that holds the advisory lock
// for the duration of Apply. SQLite has no advisory locks and its shared
// in-memory DBs (singleton tests) must not be disturbed, so the fast path
// returns a no-op unless the driver is Postgres.
type applyLock struct {
	conn *sql.Conn
}

func acquireApplyLock(ctx context.Context, db *sql.DB) (*applyLock, error) {
	if !isPostgresDriver(db.Driver()) {
		return &applyLock{}, nil
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("migration lock conn: %w", err)
	}
	// The two-argument form namespaces the key away from any app-int key.
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1,$2)", int64(1), int64(0x6c697465)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("migration advisory lock: %w", err)
	}
	return &applyLock{conn: conn}, nil
}

// isPostgresDriver identifies the Postgres driver (jackc/pgx stdlib registers
// a *stdlib.Driver instance). SQLite drivers (modernc *sqlite.Driver) return
// false, so the shared in-memory databases used by tests are never touched by
// the advisory-lock probe.
func isPostgresDriver(driver driver.Driver) bool {
	return strings.Contains(fmt.Sprintf("%T", driver), "stdlib.Driver")
}

func (l *applyLock) release() {
	if l.conn == nil {
		return
	}
	_, _ = l.conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1,$2)", int64(1), int64(0x6c697465))
	_ = l.conn.Close()
}

func applyOne(ctx context.Context, db *sql.DB, name string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	defer tx.Rollback()

	var applied string
	err = tx.QueryRowContext(ctx, "SELECT version FROM schema_migrations WHERE version = "+placeholder(db, 1), name).Scan(&applied)
	if err == nil {
		return tx.Commit()
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("check migration %s: %w", name, err)
	}

	contents, err := files.ReadFile(name)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, string(contents)); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES ("+placeholder(db, 1)+")", name); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}
	return nil
}

func placeholder(db *sql.DB, position int) string {
	// database/sql does not expose the driver name. PostgreSQL rejects '?', while
	// SQLite accepts '$1', so numeric placeholders are portable across both adapters.
	return fmt.Sprintf("$%d", position)
}

// Versions returns the embedded migration names for diagnostics and tests.
func Versions() []string {
	entries, _ := fs.Glob(files, "*.sql")
	sort.Strings(entries)
	return append([]string(nil), entries...)
}

// Owner returns the required owner metadata from an embedded migration.
func Owner(name string) (string, error) {
	contents, err := files.ReadFile(name)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if value, ok := strings.CutPrefix(line, "-- owner:"); ok {
			return strings.TrimSpace(value), nil
		}
		break
	}
	return "", fmt.Errorf("migration %s has no owner metadata", name)
}
