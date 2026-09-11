package sqlite

import (
	"context"
	guardraildomain "github.com/F31/liteAIG/internal/guardrail"
	"github.com/F31/liteAIG/internal/guardrail/builtin"
	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/internal/tenancy"
	"github.com/F31/liteAIG/migrations"
	"testing"
)

func TestGuardrailPolicyPersistsAcrossRegistryRestart(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, "file:guardrail-policy?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	tenantID := "62000000-0000-4000-8000-000000000001"
	if _, err := db.Exec(`INSERT INTO tenants(id,public_ref,name) VALUES ($1,'guardrail-ref','Guardrail')`, tenantID); err != nil {
		t.Fatal(err)
	}
	store := sqlrepo.NewGuardrailPolicyStore(db)
	scope := tenancy.TenantScope{TenantID: tenantID}
	registry := guardraildomain.NewPolicyRegistryWithStore(nil, store)

	rules := []builtin.Rule{{ID: "deny", Kind: "keyword", Pattern: "blocked", Action: "block"}}
	first, err := registry.FastPublish(ctx, scope, guardraildomain.Policy{ID: "policy", TenantID: tenantID, Rules: rules}, guardraildomain.ChangeTighten, "admin")
	if err != nil || first.Version != 1 || first.SecurityEpoch != 1 {
		t.Fatalf("publish = %+v, %v", first, err)
	}
	second, err := registry.FastPublish(ctx, scope, guardraildomain.Policy{ID: "policy", TenantID: tenantID, Rules: rules}, guardraildomain.ChangeLoosen, "admin")
	if err != nil || second.Version != 2 || second.SecurityEpoch != 2 {
		t.Fatalf("second publish = %+v, %v", second, err)
	}

	// A fresh registry loads the active policy from storage.
	fresh := guardraildomain.NewPolicyRegistryWithStore(nil, store)
	active, ok := fresh.Active(ctx, scope)
	if !ok || active.Version != 2 || active.SecurityEpoch != 2 || len(active.Rules) != 1 {
		t.Fatalf("reloaded active = %+v, %t", active, ok)
	}
	// Cross-tenant isolation.
	if _, err := store.GetActive(ctx, tenancy.TenantScope{TenantID: "other"}); err != tenancy.ErrNotFound {
		t.Fatalf("cross-tenant GetActive error = %v", err)
	}
}
