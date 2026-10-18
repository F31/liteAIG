package postgres

import (
	"context"
	"database/sql"
	"testing"

	"github.com/F31/liteAIG/internal/platform/storage/postgres/testutil"
	"github.com/F31/liteAIG/internal/tenancy"
	"github.com/F31/liteAIG/migrations"
)

// rlsTestConn returns a connection already set to the restricted app role for a
// tenant within a transaction, so SET LOCAL applies.
func rlsAppConn(t *testing.T, db *sql.DB, tenantID string) *sql.Conn {
	t.Helper()
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := conn.ExecContext(context.Background(), `BEGIN`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(context.Background(), `SET ROLE `+RoleApp); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(context.Background(), `SET LOCAL app.tenant_id = '`+tenantID+`'`); err != nil {
		t.Fatal(err)
	}
	return conn
}

func TestRLSBlocksLateralTenantMovement(t *testing.T) {
	db := openFreshDatabase(t)
	defer db.Close()
	ctx := context.Background()
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	tenantA := "b1000000-0000-4000-8000-000000000001"
	tenantB := "b1000000-0000-4000-8000-000000000002"
	projectA := "b1000000-0000-4000-8000-000000000003"
	projectB := "b1000000-0000-4000-8000-000000000004"
	for _, tup := range [][2]string{{tenantA, "ref-a"}, {tenantB, "ref-b"}} {
		if _, err := db.Exec(`INSERT INTO tenants(id, public_ref, name) VALUES ($1, $2, 'T')`, tup[0], tup[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, tup := range [][2]string{{projectA, tenantA}, {projectB, tenantB}} {
		if _, err := db.Exec(`INSERT INTO projects(id, tenant_id, name) VALUES ($1, $2, 'P')`, tup[0], tup[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := EnableRLS(ctx, db); err != nil {
		t.Fatal(err)
	}

	// Tenant A, restricted app role, must see only its own project.
	conn := rlsAppConn(t, db, tenantA)
	var count int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("tenant A sees %d projects; RLS failed to isolate", count)
	}
	var id string
	if err := conn.QueryRowContext(ctx, `SELECT id FROM projects`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id != projectA {
		t.Fatalf("tenant A saw project %s", id)
	}
}

func TestRLSPlatformRoleBypassesForControlledOps(t *testing.T) {
	db := openFreshDatabase(t)
	defer db.Close()
	ctx := context.Background()
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	tenantA := "b2000000-0000-4000-8000-000000000001"
	tenantB := "b2000000-0000-4000-8000-000000000002"
	for _, ref := range []string{"ref-a", "ref-b"} {
		var id string
		if ref == "ref-a" {
			id = tenantA
		} else {
			id = tenantB
		}
		if _, err := db.Exec(`INSERT INTO tenants(id, public_ref, name) VALUES ($1, $2, 'T')`, id, ref); err != nil {
			t.Fatal(err)
		}
	}
	if err := EnableRLS(ctx, db); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SET ROLE `+RolePlatform); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM tenants`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("platform role sees %d tenants, want 2", count)
	}
}

var _ = sql.ErrNoRows

func TestTenantRouterSelectsRLSPoolFromContext(t *testing.T) {
	db, dsn := testutil.IsolatedDatabase(t)
	defer db.Close()
	ctx := context.Background()
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	tenantA := "b3000000-0000-4000-8000-000000000001"
	tenantB := "b3000000-0000-4000-8000-000000000002"
	for _, id := range []string{tenantA, tenantB} {
		if _, err := db.Exec(`INSERT INTO tenants(id, public_ref, name) VALUES ($1, $2, 'T')`, id, "ref-"+id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO projects(id, tenant_id, name) VALUES ($1, $2, 'P')`, id, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := EnableRLS(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := EnableRLS(ctx, db); err != nil {
		t.Fatalf("EnableRLS is not idempotent: %v", err)
	}
	router, err := NewTenantRouter(dsn, db, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	tenantCtx := tenancy.WithTenant(ctx, tenantA)
	var count int
	if err := router.QueryRowContext(tenantCtx, `SELECT COUNT(*) FROM projects`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("tenant router sees %d projects, want 1", count)
	}
	if err := router.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("platform router sees %d projects, want 2", count)
	}
	if _, err := router.ExecContext(tenantCtx, `INSERT INTO projects(id, tenant_id, name) VALUES ($1, $2, 'X')`, "b3000000-0000-4000-8000-000000000003", tenantB); err == nil {
		t.Fatal("tenant A was able to insert a project for tenant B under RLS")
	}
}
