package sqlrepo

import (
	"context"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/identity"
	"github.com/F31/liteAIG/internal/platform/storage/sqlite"
	"github.com/F31/liteAIG/internal/tenancy"
	"github.com/F31/liteAIG/migrations"
)

func delegationTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sqlite.Open(context.Background(), "file:delegation?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return Open(db, Deps{})
}

func TestDelegationStorePutListDelete(t *testing.T) {
	store := delegationTestStore(t)
	ctx := context.Background()
	scope := tenancy.TenantScope{TenantID: "tenant-a"}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO tenants(id, public_ref, name) VALUES ($1,$1,$1)`, scope.TenantID); err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 9, 8, 1, 2, 3, 0, time.UTC)
	grant, err := store.Delegations.Put(ctx, scope, identity.DelegationGrant{ID: "grant-1", DelegatorID: "agent-a", DelegateeID: "agent-b", Permissions: []string{"invoice.read"}, CreatedBy: "admin", CreatedAt: created})
	if err != nil {
		t.Fatal(err)
	}
	if grant.TenantID != scope.TenantID {
		t.Fatalf("tenant not stamped: %+v", grant)
	}
	items, err := store.Delegations.List(ctx, scope)
	if err != nil || len(items) != 1 {
		t.Fatalf("List()=%+v,%v", items, err)
	}
	if items[0].DelegatorID != "agent-a" || items[0].DelegateeID != "agent-b" || len(items[0].Permissions) != 1 || items[0].Permissions[0] != "invoice.read" {
		t.Fatalf("grant row = %+v", items[0])
	}
	ok, err := store.Delegations.Delete(ctx, scope, "grant-1")
	if err != nil || !ok {
		t.Fatalf("Delete() ok=%v err=%v", ok, err)
	}
	ok, err = store.Delegations.Delete(ctx, scope, "grant-1")
	if err != nil || ok {
		t.Fatalf("second Delete() ok=%v err=%v", ok, err)
	}
}
