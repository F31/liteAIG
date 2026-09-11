// Package sqlite provides the Lite connection driver: it opens a SQLite
// handle with the pragmas and pool bounds the profile needs. Repository
// construction lives in sqlrepo (see sqlrepo.Open); this package never
// constructs domain repositories.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// Open creates a SQLite connection with foreign keys and bounded lock waiting
// enabled. File-backed databases additionally run in WAL mode (concurrent
// readers plus one writer, synchronous=NORMAL) with a bounded connection
// pool; in-memory databases cannot use WAL and stay as-is.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("sqlite DSN is required")
	}
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	pragmas := "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	if isFileDSN(dsn) {
		pragmas += "&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	}
	db, err := sql.Open("sqlite", dsn+separator+pragmas)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if isFileDSN(dsn) {
		db.SetMaxIdleConns(4)
		db.SetMaxOpenConns(8)
	} else {
		// In-memory (mode=memory) databases are shared by URI and their content
		// is dropped when the last open connection closes. Pinning a single
		// connection for the pool's lifetime makes the database deterministic
		// and immune to connection churn under concurrent tests/goroutines.
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	}
	return db, nil
}

// isFileDSN reports whether the DSN addresses a persistent file. In-memory
// databases (":memory:", empty file: paths, mode=memory) cannot use WAL.
func isFileDSN(dsn string) bool {
	base := dsn
	if i := strings.IndexAny(base, "?#"); i >= 0 {
		base = base[:i]
	}
	base = strings.TrimSpace(strings.TrimPrefix(base, "file:"))
	if base == "" || base == ":memory:" {
		return false
	}
	if i := strings.Index(dsn, "?"); i >= 0 && strings.Contains(dsn[i+1:], "mode=memory") {
		return false
	}
	return true
}
