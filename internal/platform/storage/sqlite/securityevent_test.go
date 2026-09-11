package sqlite

import (
	"context"
	"github.com/F31/liteAIG/internal/guardrail"
	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/internal/tenancy"
	"github.com/F31/liteAIG/migrations"
	"strings"
	"testing"
	"time"
)

func TestSecurityEventPersistenceIsScopedAndRedacted(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, "file:security-events?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	tenantID := "61000000-0000-4000-8000-000000000001"
	projectID := "61000000-0000-4000-8000-000000000002"
	if _, err := db.Exec(`INSERT INTO tenants(id,public_ref,name) VALUES ($1,'security-ref','Security')`, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects(id,tenant_id,name) VALUES ($1,$2,'Default')`, projectID, tenantID); err != nil {
		t.Fatal(err)
	}
	store := sqlrepo.NewSecurityEventStore(db)
	event := guardrail.SecurityEvent{ID: "61000000-0000-4000-8000-000000000003", TenantID: tenantID, ProjectID: projectID, PolicyID: "policy", RuleID: "rule", Action: "block", ContentHash: "sha256-only", SnapshotVersion: 2, OccurredAt: time.Unix(2, 0)}
	if err := store.Create(ctx, tenancy.TenantScope{TenantID: tenantID}, event); err != nil {
		t.Fatal(err)
	}
	items, err := store.List(ctx, tenancy.TenantScope{TenantID: tenantID}, 10)
	if err != nil || len(items) != 1 || items[0].ContentHash != "sha256-only" {
		t.Fatalf("List() = %+v, %v", items, err)
	}
	foreign, err := store.List(ctx, tenancy.TenantScope{TenantID: "other"}, 10)
	if err != nil || len(foreign) != 0 {
		t.Fatalf("cross-tenant List() = %+v, %v", foreign, err)
	}
	var schema string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE name='security_events'`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(schema), "prompt") || strings.Contains(strings.ToLower(schema), "response_body") {
		t.Fatalf("sensitive content column in schema: %s", schema)
	}
}
