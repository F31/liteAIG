package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/federation"
	"github.com/F31/liteAIG/internal/platform/storage/sqlite"
	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/internal/tenancy"
	"github.com/F31/liteAIG/migrations"
)

// TestFederationRelationshipsSurviveRestart proves lifecycle state (suspend /
// revoke) persists across process restarts.
func TestFederationRelationshipsSurviveRestart(t *testing.T) {
	ctx := context.Background()
	dsn := "file:" + filepath.Join(t.TempDir(), "federation.db")
	scope := tenancy.TenantScope{TenantID: "00000000-0000-4000-8000-000000000000"}
	relationship := federation.Relationship{
		ID: "00000000-0000-4000-8000-000000000001", TenantID: scope.TenantID,
		ExternalAgentID: "ext-1", Name: "Partner", ExternalSubject: "partner.example",
		Anchors:          []federation.TrustAnchor{{ID: "a1", Type: federation.AnchorMTLS, Subject: "spki:x", Verified: true}},
		ProjectGrants:    []federation.ProjectGrant{{ID: "pg1", ProjectID: "project-1"}},
		CapabilityGrants: []federation.CapabilityGrant{{ID: "cg1", Capability: "invoice.read"}},
		CreatedAt:        time.Now().UTC(),
	}

	db, err := sqlite.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(ctx, db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO tenants(id, public_ref, name, status)
VALUES ('00000000-0000-4000-8000-000000000000','acme','Acme','active')`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	store := sqlrepo.NewFederationStore(db)

	first := federation.NewLifecycleWithStore(time.Now, store)
	if _, err := first.Activate(ctx, relationship); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := first.Suspend(ctx, scope, relationship.ID); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	secondDB, err := sqlite.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer secondDB.Close()
	second := federation.NewLifecycleWithStore(time.Now, sqlrepo.NewFederationStore(secondDB))
	loaded, ok := second.Get(ctx, scope, relationship.ID)
	if !ok {
		t.Fatal("relationship lost across restart")
	}
	if loaded.Status != federation.StatusSuspended {
		t.Fatalf("status after restart = %s, want suspended", loaded.Status)
	}
	if second.Active(scope, relationship.ID) {
		t.Fatal("suspended relationship reported active after restart")
	}
}
