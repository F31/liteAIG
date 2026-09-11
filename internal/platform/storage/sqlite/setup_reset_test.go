package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/migrations"
)

func openStore(t *testing.T) (*sql.DB, *sqlrepo.Store) {
	t.Helper()
	db, err := Open(context.Background(), "file:setup?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db, sqlrepo.Open(db, sqlrepo.Deps{IDs: &counterIDs{}})
}

type counterIDs struct{ next int }

func (c *counterIDs) New() (string, error) {
	c.next++
	return fmt.Sprintf("id-%d", c.next), nil
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// insertHalfSetup mirrors exactly what the wizard commits first: one admin,
// one tenant, the default project, and (optionally) a wizard-created key.
func insertHalfSetup(t *testing.T, db *sql.DB, withKey bool) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO local_admins(id, username, password_hash, role, status, created_at) VALUES ('a','admin','hash','tenant_admin','active',CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tenants(id, public_ref, name, status, settlement_currency, default_project_id, created_at) VALUES ('t','ref1','Acme','active','USD','p',CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects(id, tenant_id, name, allowed_data_regions, residency_enforcement, status, created_at) VALUES ('p','t','Default','["global"]','advisory','active',CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if withKey {
		if _, err := db.Exec(`INSERT INTO api_keys(id, tenant_id, project_id, public_id, name, hmac_digest, pepper_version, fingerprint, status, created_at) VALUES ('k','t','p','pub','first-key','digest',1,'fp','active',CURRENT_TIMESTAMP)`); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResetHalfInitializedClearsInterruptedWizard(t *testing.T) {
	ctx := context.Background()
	db, store := openStore(t)
	bootstrap := store.Bootstrap

	// A key created before the failed publish must not block the reset.
	insertHalfSetup(t, db, true)

	cleaned, err := bootstrap.ResetHalfInitialized(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !cleaned {
		t.Fatal("expected the half-initialized state to be resumable")
	}
	for _, table := range []string{"local_admins", "tenants", "projects", "api_keys"} {
		if got := countRows(t, db, table); got != 0 {
			t.Fatalf("%s rows = %d after reset, want 0", table, got)
		}
	}
}

func TestResetHalfInitializedRefusesRealData(t *testing.T) {
	ctx := context.Background()
	db, store := openStore(t)
	bootstrap := store.Bootstrap

	// A published config version means the install finished: never wipe it.
	insertHalfSetup(t, db, false)
	if _, err := db.Exec(`INSERT INTO config_versions(id, scope_type, tenant_id, version, source_draft_id, compiled_config, published_by, published_at) VALUES ('v','tenant','t',1,NULL,'{}','a',CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}

	cleaned, err := bootstrap.ResetHalfInitialized(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cleaned {
		t.Fatal("reset must refuse an install that already published a config version")
	}
	if countRows(t, db, "local_admins") != 1 || countRows(t, db, "config_versions") != 1 {
		t.Fatal("reset modified a non-resumable state")
	}
}

func TestAuditListReturnsActorAndTimestamp(t *testing.T) {
	ctx := context.Background()
	db, store := openStore(t)
	if _, err := db.Exec(`INSERT INTO tenants(id, public_ref, name, status, settlement_currency, default_project_id, created_at) VALUES ('tenant-1','ref1','Acme','active','USD','p',CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO local_admins(id, username, password_hash, role, status, created_at) VALUES ('actor-1','alice','hash','tenant_admin','active',CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	recorder := sqlrepo.NewAuditRecorder(db, &counterIDs{})
	if err := recorder.Record(ctx, "tenant-1", "actor-1", "user.role_change", "local_user", "target-1"); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Record(ctx, "tenant-1", "anonymous", "auth.login_failed", "local_user", "alice"); err != nil {
		t.Fatal(err)
	}

	records, err := store.Audit.List(ctx, "tenant-1", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	// Newest first: the anonymous login failure then the role change.
	if records[0].Actor != "anonymous" || records[0].Action != "auth.login_failed" {
		t.Fatalf("records[0] = %+v", records[0])
	}
	if records[1].Actor != "alice" || records[1].ResourceType != "local_user" || records[1].ResourceID != "target-1" {
		t.Fatalf("records[1] = %+v", records[1])
	}
	for _, record := range records {
		if record.OccurredAt.IsZero() {
			t.Fatalf("occurred_at not populated: %+v", record)
		}
	}
}
