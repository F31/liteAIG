package sqlrepo

import (
	"context"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/controlplane/rbac"
	"github.com/F31/liteAIG/internal/platform/storage/sqlite"
	"github.com/F31/liteAIG/migrations"
)

func localAuthStore(t *testing.T, name string) *LocalCredentialStore {
	t.Helper()
	db, err := sqlite.Open(context.Background(), "file:local-auth-"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return NewLocalCredentialStore(db)
}

func TestSystemAdminCredentialHasNoImplicitTenant(t *testing.T) {
	store := localAuthStore(t, "system-admin")
	if _, err := store.db.ExecContext(context.Background(), `INSERT INTO local_admins(id, username, password_hash, role, status)
VALUES ('00000000-0000-4000-8000-000000000001', 'root', 'hash', 'system_admin', 'active')`); err != nil {
		t.Fatal(err)
	}

	credential, err := store.FindLocalCredential(context.Background(), "root")
	if err != nil {
		t.Fatalf("FindLocalCredential() error = %v", err)
	}
	if credential.Role != rbac.RoleSystemAdmin || credential.TenantID != "" {
		t.Fatalf("credential role=%q tenant=%q", credential.Role, credential.TenantID)
	}

	byID, err := store.FindLocalUserByID(context.Background(), credential.AdminID)
	if err != nil {
		t.Fatalf("FindLocalUserByID() error = %v", err)
	}
	if byID.Role != rbac.RoleSystemAdmin || byID.TenantID != "" {
		t.Fatalf("byID role=%q tenant=%q", byID.Role, byID.TenantID)
	}
}

func TestTenantAdminCredentialStillRequiresActiveTenant(t *testing.T) {
	store := localAuthStore(t, "tenant-admin-no-tenant")
	if _, err := store.db.ExecContext(context.Background(), `INSERT INTO local_admins(id, username, password_hash, role, status)
VALUES ('00000000-0000-4000-8000-000000000002', 'admin', 'hash', 'tenant_admin', 'active')`); err != nil {
		t.Fatal(err)
	}

	if _, err := store.FindLocalCredential(context.Background(), "admin"); err == nil {
		t.Fatal("FindLocalCredential() error = nil, want missing active tenant")
	}
}

func TestHasActiveTenantMembership(t *testing.T) {
	store := localAuthStore(t, "membership")
	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx, `INSERT INTO tenants(id, public_ref, name, status)
VALUES ('00000000-0000-4000-8000-000000000010', 'tenant-ref', 'Tenant', 'active')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO users(id, display_name, status)
VALUES ('00000000-0000-4000-8000-000000000011', 'User', 'active')`); err != nil {
		t.Fatal(err)
	}
	checker := NewOrganizationRepository(store.db)
	if ok, err := checker.HasActiveTenantMembership(ctx, "00000000-0000-4000-8000-000000000011", "00000000-0000-4000-8000-000000000010"); err != nil || ok {
		t.Fatalf("membership before insert = %v,%v", ok, err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO tenant_memberships(tenant_id, user_id, status)
VALUES ('00000000-0000-4000-8000-000000000010', '00000000-0000-4000-8000-000000000011', 'active')`); err != nil {
		t.Fatal(err)
	}
	if ok, err := checker.HasActiveTenantMembership(ctx, "00000000-0000-4000-8000-000000000011", "00000000-0000-4000-8000-000000000010"); err != nil || !ok {
		t.Fatalf("membership after insert = %v,%v", ok, err)
	}
}

func TestBatchMappingStore(t *testing.T) {
	store := localAuthStore(t, "batch-mapping")
	batchStore := NewBatchMappingStore(store.db)
	ctx := context.Background()
	if err := batchStore.SaveBatchMapping(ctx, "batch_1", "batch-logical"); err != nil {
		t.Fatal(err)
	}
	model, err := batchStore.ModelForBatch(ctx, "batch_1")
	if err != nil || model != "batch-logical" {
		t.Fatalf("ModelForBatch() = %q, %v", model, err)
	}
	if _, err := batchStore.ModelForBatch(ctx, "missing"); err == nil {
		t.Fatal("missing batch mapping returned nil error")
	}
	if err := batchStore.SaveBatchFileMapping(ctx, "batch_1", "file_out", "batch-logical"); err != nil {
		t.Fatal(err)
	}
	model, err = batchStore.ModelForBatchFile(ctx, "file_out")
	if err != nil || model != "batch-logical" {
		t.Fatalf("ModelForBatchFile() = %q, %v", model, err)
	}
	if _, err := batchStore.ModelForBatchFile(ctx, "missing"); err == nil {
		t.Fatal("missing batch file mapping returned nil error")
	}
	if err := batchStore.DeleteFileMapping(ctx, "file_out"); err != nil {
		t.Fatal(err)
	}
	if _, err := batchStore.ModelForFile(ctx, "file_out"); err == nil {
		t.Fatal("deleted file mapping remained readable")
	}
	if err := batchStore.SaveFileMapping(ctx, "file_old", "batch-logical", "upload", ""); err != nil {
		t.Fatal(err)
	}
	if err := batchStore.SaveFileMapping(ctx, "file_new", "batch-logical", "upload", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE file_mappings SET created_at=$1 WHERE file_id=$2`, time.Unix(100, 0).UTC(), "file_old"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE file_mappings SET created_at=$1 WHERE file_id=$2`, time.Unix(300, 0).UTC(), "file_new"); err != nil {
		t.Fatal(err)
	}
	removed, err := batchStore.PurgeFileMappingsOlderThan(ctx, time.Unix(200, 0).UTC())
	if err != nil || removed != 1 {
		t.Fatalf("PurgeFileMappingsOlderThan() = %d, %v", removed, err)
	}
	if _, err := batchStore.ModelForFile(ctx, "file_old"); err == nil {
		t.Fatal("old file mapping survived purge")
	}
	if model, err := batchStore.ModelForFile(ctx, "file_new"); err != nil || model != "batch-logical" {
		t.Fatalf("new file mapping after purge = %q, %v", model, err)
	}
}
