package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	controlconfig "github.com/F31/liteAIG/internal/controlplane/config"
	setupcontract "github.com/F31/liteAIG/internal/controlplane/setup/contracttest"
	"github.com/F31/liteAIG/internal/finops/accounting"
	"github.com/F31/liteAIG/internal/identity/apikey"
	"github.com/F31/liteAIG/internal/organization"
	"github.com/F31/liteAIG/internal/tenancy"
	"github.com/F31/liteAIG/internal/tenancy/contracttest"
	"github.com/F31/liteAIG/migrations"
)

func TestRepositoryConformance(t *testing.T) {
	db := openFreshDatabase(t)
	defer db.Close()
	for i := 0; i < 2; i++ {
		if err := migrations.Apply(context.Background(), db); err != nil {
			t.Fatalf("Apply() pass %d error = %v", i+1, err)
		}
	}

	contracttest.Run(t, db, NewTenancyRepository(db), NewTenancyTransactor(db))
	contracttest.RunTenantAdmin(t, NewTenancyAdmin(db))
}

func TestConfigDraftAndPublishPersistence(t *testing.T) {
	db := openFreshDatabase(t)
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	tenantID := "60000000-0000-4000-8000-000000000001"
	if _, err := db.Exec(`INSERT INTO tenants(id, public_ref, name) VALUES ($1, 'pg-config-ref', 'PG Config')`, tenantID); err != nil {
		t.Fatal(err)
	}
	repository := NewConfigRepository(db)
	scope := tenancy.TenantScope{TenantID: tenantID}
	draft := controlconfig.Draft{ID: "60000000-0000-4000-8000-000000000002", TenantID: tenantID, Revision: 1, Status: "editing", Config: controlconfig.TenantConfig{SchemaVersion: controlconfig.SchemaV1, Tenant: controlconfig.TenantResource{ID: tenantID, PublicRef: "pg-config-ref", Status: "active"}}, CreatedBy: "60000000-0000-4000-8000-000000000003", UpdatedBy: "60000000-0000-4000-8000-000000000003", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := repository.CreateDraft(context.Background(), scope, draft); err != nil {
		t.Fatal(err)
	}
	version, err := repository.PublishDraft(context.Background(), scope, controlconfig.PublishRecord{DraftID: draft.ID, VersionID: "60000000-0000-4000-8000-000000000004", AuditID: "60000000-0000-4000-8000-000000000005", ActorID: draft.CreatedBy, ExpectedRevision: 1, PublishedAt: time.Now()})
	if err != nil || version.Version != 1 {
		t.Fatalf("PublishDraft() = %+v, %v", version, err)
	}
	var audits int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE action='config.publish'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("audit count = %d, %v", audits, err)
	}
}

func TestAPIKeyDigestPersistence(t *testing.T) {
	db := openFreshDatabase(t)
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	tenantID := "80000000-0000-4000-8000-000000000001"
	projectID := "80000000-0000-4000-8000-000000000002"
	if _, err := db.Exec(`INSERT INTO tenants(id,public_ref,name) VALUES ($1,'pg-key-ref','PG Key')`, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects(id,tenant_id,name) VALUES ($1,$2,'Default')`, projectID, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO key_pepper_versions(version,pepper_ref,status) VALUES (1,'secret://pepper/1','active')`); err != nil {
		t.Fatal(err)
	}
	repository := NewAPIKeyRepository(db)
	expiry := time.Now().Add(time.Hour)
	record := apikey.Record{ID: "80000000-0000-4000-8000-000000000003", PublicID: "11111111111111111111111111111111", TenantID: tenantID, ProjectID: projectID, Name: "key", HMACDigest: []byte{1, 2, 3}, PepperVersion: 1, Fingerprint: "fingerprint", Status: "active", ExpiresAt: &expiry, ModelAllowlist: []string{"default-chat"}, CreatedAt: time.Now()}
	if err := repository.Create(context.Background(), tenancy.TenantScope{TenantID: tenantID}, record); err != nil {
		t.Fatal(err)
	}
	items, err := repository.ListActive(context.Background(), tenancy.TenantScope{TenantID: tenantID})
	if err != nil || len(items) != 1 || items[0].HMACDigest[0] != 1 || items[0].ExpiresAt == nil {
		t.Fatalf("ListActive()=%+v,%v", items, err)
	}
}

func TestAccountingFinalizationPersistence(t *testing.T) {
	db := openFreshDatabase(t)
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	tenantID := "91000000-0000-4000-8000-000000000001"
	projectID := "91000000-0000-4000-8000-000000000002"
	if _, err := db.Exec(`INSERT INTO tenants(id,public_ref,name) VALUES ($1,'pg-account','Account')`, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects(id,tenant_id,name) VALUES ($1,$2,'Default')`, projectID, tenantID); err != nil {
		t.Fatal(err)
	}
	repository := NewAccountingRepository(db)
	facts := accounting.Facts{UsageEventID: "91000000-0000-4000-8000-000000000003", RequestID: "91000000-0000-4000-8000-000000000004", TenantID: tenantID, ProjectID: projectID, LogicalModel: "chat", Outcome: "failed", UsageSource: "api", SnapshotVersion: 1, SecurityEpoch: 1, ReceivedAt: time.Unix(1, 0), CompletedAt: time.Unix(2, 0)}
	created, err := repository.Finalize(context.Background(), facts)
	if err != nil || !created {
		t.Fatalf("Finalize()=%t,%v", created, err)
	}
	created, err = repository.Finalize(context.Background(), facts)
	if err != nil || created {
		t.Fatalf("duplicate Finalize()=%t,%v", created, err)
	}
}

func TestBootstrapRepositoryConformance(t *testing.T) {
	db := openFreshDatabase(t)
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	setupcontract.Run(t, db, NewBootstrapRepository(db))
}

func TestOrganizationEffectiveDatedReassignment(t *testing.T) {
	db := openFreshDatabase(t)
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	tenantID := "a5000000-0000-4000-8000-000000000001"
	userID := "a5000000-0000-4000-8000-000000000002"
	orgA := "a5000000-0000-4000-8000-000000000003"
	orgB := "a5000000-0000-4000-8000-000000000004"
	if _, err := db.Exec(`INSERT INTO tenants(id, public_ref, name) VALUES ($1, 'pg-org-ref', 'PG Org')`, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users(id, display_name) VALUES ($1, 'Alice')`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tenant_memberships(tenant_id, user_id) VALUES ($1, $2)`, tenantID, userID); err != nil {
		t.Fatal(err)
	}
	repository := NewOrganizationRepository(db)
	scope := tenancy.TenantScope{TenantID: tenantID}
	now := time.Unix(1000, 0)
	for _, unit := range []organization.OrgUnit{
		{ID: orgA, TenantID: tenantID, Name: "Org A", Path: "/a", Status: "active", CreatedAt: now},
		{ID: orgB, TenantID: tenantID, Name: "Org B", Path: "/b", Status: "active", CreatedAt: now},
	} {
		if err := repository.CreateOrgUnit(context.Background(), scope, unit); err != nil {
			t.Fatal(err)
		}
	}
	end := time.Unix(1100, 0)
	if err := repository.AssignUser(context.Background(), scope, organization.UserOrgAssignment{ID: "a5000000-0000-4000-8000-000000000005", TenantID: tenantID, UserID: userID, OrgUnitID: orgA, IsPrimary: true, ValidFrom: now, ValidTo: &end, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := repository.AssignUser(context.Background(), scope, organization.UserOrgAssignment{ID: "a5000000-0000-4000-8000-000000000006", TenantID: tenantID, UserID: userID, OrgUnitID: orgB, IsPrimary: true, ValidFrom: end, ValidTo: nil, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	early, err := repository.ActiveAssignment(context.Background(), scope, userID, time.Unix(1050, 0))
	if err != nil || early.OrgUnitID != orgA {
		t.Fatalf("early = %+v, %v", early, err)
	}
	late, err := repository.ActiveAssignment(context.Background(), scope, userID, time.Unix(1200, 0))
	if err != nil || late.OrgUnitID != orgB {
		t.Fatalf("late = %+v, %v", late, err)
	}
}

// openFreshDatabase returns a dedicated per-test database so destructive
// conformance reset (DROP SCHEMA) can never collide with another package
// running migrations against the shared LITEAIG_TEST_POSTGRES_DSN database
// (chaos/smoke tests). The temporary database is dropped on cleanup.
func openFreshDatabase(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("LITEAIG_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("LITEAIG_TEST_POSTGRES_DSN is not configured")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := "liteaig_cf_" + fmt.Sprintf("%d_%d", time.Now().UnixNano(), os.Getpid())
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
	t.Cleanup(func() {
		cleanupDB, err := sql.Open("pgx", admin.String())
		if err != nil {
			return
		}
		defer cleanupDB.Close()
		_, _ = cleanupDB.Exec("DROP DATABASE IF EXISTS \"" + name + "\" WITH (FORCE)")
	})
	db, err := Open(context.Background(), created.String())
	if err != nil {
		t.Fatal(err)
	}
	return db
}
