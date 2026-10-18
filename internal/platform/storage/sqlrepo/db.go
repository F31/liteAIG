package sqlrepo

import (
	"context"
	"database/sql"
)

// DBTX is the subset of *sql.DB the repositories use. It lets the Postgres
// tenant router select a role/tenant-scoped pool per call while keeping the
// SQLite adapter on a plain *sql.DB.
type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}
