// Package testutil provides Postgres test isolation helpers shared by several
// packages that exercise Standard-tier behavior against LITEAIG_TEST_POSTGRES_DSN.
package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// IsolatedDatabase returns a dedicated per-test database so destructive reset
// (or the master-key material of another test) can never collide with a
// different package running against the shared LITEAIG_TEST_POSTGRES_DSN
// database. It returns the open handle and the DSN of the isolated database.
// The temporary database is dropped when the test completes.
func IsolatedDatabase(t interface {
	Helper()
	Cleanup(func())
	Fatal(...any)
}) (*sql.DB, string) {
	t.Helper()
	dsn := os.Getenv("LITEAIG_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("LITEAIG_TEST_POSTGRES_DSN is not configured")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := "liteaig_tu_" + fmt.Sprintf("%d_%d", time.Now().UnixNano(), os.Getpid())
	admin := *u
	admin.Path = "/postgres"
	adminDB, err := sql.Open("pgx", admin.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adminDB.Exec("CREATE DATABASE \"" + name + "\""); err != nil {
		adminDB.Close()
		t.Fatal(err)
	}
	adminDB.Close()
	created := *u
	created.Path = "/" + name
	freshDSN := created.String()
	t.Cleanup(func() {
		cleanupDB, err := sql.Open("pgx", admin.String())
		if err != nil {
			return
		}
		defer cleanupDB.Close()
		_, _ = cleanupDB.Exec("DROP DATABASE IF EXISTS \"" + name + "\" WITH (FORCE)")
	})
	db, err := sql.Open("pgx", freshDSN)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db, freshDSN
}
