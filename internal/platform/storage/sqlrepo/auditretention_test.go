package sqlrepo

import (
	"context"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/platform/storage/sqlite"
	"github.com/F31/liteAIG/migrations"
)

func auditRetentionStore(t *testing.T) *Store {
	t.Helper()
	db, err := sqlite.Open(context.Background(), "file:audit-retention?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	store := Open(db, Deps{})
	seedRetentionTenants(t, store)
	return store
}

func seedRetentionTenants(t *testing.T, store *Store) {
	t.Helper()
	for _, tenant := range []struct{ id, ref, name string }{
		{"44444444-4444-4444-8444-444444444444", "ret-a", "Retention A"},
		{"55555555-5555-4555-8555-555555555555", "ret-b", "Retention B"},
	} {
		if _, err := store.db.Exec(`INSERT INTO tenants(id, public_ref, name, status, created_at) VALUES ($1,$2,$3,'active',CURRENT_TIMESTAMP)`, tenant.id, tenant.ref, tenant.name); err != nil {
			t.Fatalf("seed tenant %q: %v", tenant.name, err)
		}
	}
}

func seedRetentionAudit(t *testing.T, store *Store, tenant string, ages []time.Duration) {
	t.Helper()
	for i, age := range ages {
		id := string(rune('a'+i)) + tenant[0:4] + "00000000000000000000000000"
		ts := time.Now().UTC().Add(-age)
		if _, err := store.db.Exec(`
INSERT INTO audit_events(id, tenant_id, scope, actor_id, action, resource_type, result, occurred_at)
VALUES ($1,$2,'tenant','actor','test.save','audit','success',$3)`, id, tenant, ts); err != nil {
			t.Fatalf("seed audit event: %v", err)
		}
	}
}

func TestAuditRetentionUnsetReturnsZeroPolicy(t *testing.T) {
	store := auditRetentionStore(t)
	ctx := context.Background()
	policy, err := store.AuditRetention.Get(ctx, "44444444-4444-4444-8444-444444444444")
	if err != nil {
		t.Fatal(err)
	}
	if policy.RetentionDays != 0 {
		t.Fatalf("unset policy = %+v, want zero", policy)
	}
}

func TestAuditRetentionSetGetList(t *testing.T) {
	store := auditRetentionStore(t)
	ctx := context.Background()
	tenant := "44444444-4444-4444-8444-444444444444"
	if err := store.AuditRetention.Set(ctx, tenant, 365, "admin-1"); err != nil {
		t.Fatal(err)
	}
	policy, err := store.AuditRetention.Get(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if policy.RetentionDays != 365 || policy.UpdatedBy != "admin-1" {
		t.Fatalf("policy = %+v", policy)
	}
	policies, err := store.AuditRetention.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != 1 || policies[0].TenantID != tenant {
		t.Fatalf("policies = %+v", policies)
	}
}

func TestAuditRetentionDisableRemovesPolicy(t *testing.T) {
	store := auditRetentionStore(t)
	ctx := context.Background()
	tenant := "44444444-4444-4444-8444-444444444444"
	if err := store.AuditRetention.Set(ctx, tenant, 30, "admin-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.AuditRetention.Set(ctx, tenant, 0, "admin-1"); err != nil {
		t.Fatal(err)
	}
	policy, err := store.AuditRetention.Get(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if policy.RetentionDays != 0 {
		t.Fatalf("disabled policy = %+v, want zero", policy)
	}
}

func TestAuditRetentionPurgeDeletesOnlyExpired(t *testing.T) {
	store := auditRetentionStore(t)
	ctx := context.Background()
	tenant := "44444444-4444-4444-8444-444444444444"
	seedRetentionAudit(t, store, tenant, []time.Duration{400 * 24 * time.Hour, 2 * time.Hour})
	seedRetentionAudit(t, store, "55555555-5555-4555-8555-555555555555", []time.Duration{400 * 24 * time.Hour})

	removed, err := store.AuditRetention.Purge(ctx, tenant, time.Now().Add(-365*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("purge removed %d, want 1 (only the 400-day-old tenant row)", removed)
	}

	var remaining int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE tenant_id=$1`, tenant).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("remaining = %d, want 1", remaining)
	}
	var other int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE tenant_id=$1`, "55555555-5555-4555-8555-555555555555").Scan(&other); err != nil {
		t.Fatal(err)
	}
	if other != 1 {
		t.Fatalf("other tenant rows = %d, want 1 (cross-tenant isolation)", other)
	}
}

func TestAuditRetentionCrossTenantIsolation(t *testing.T) {
	store := auditRetentionStore(t)
	ctx := context.Background()
	tenantA := "44444444-4444-4444-8444-444444444444"
	tenantB := "55555555-5555-4555-8555-555555555555"
	if err := store.AuditRetention.Set(ctx, tenantA, 30, "admin-1"); err != nil {
		t.Fatal(err)
	}
	policies, err := store.AuditRetention.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range policies {
		if policy.TenantID != tenantA {
			t.Fatalf("retention leaked to %q", policy.TenantID)
		}
	}
	if _, err := store.AuditRetention.Get(ctx, tenantB); err != nil {
		t.Fatal(err)
	}
}
