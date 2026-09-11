// Package contracttest verifies atomic setup repository behavior across databases.
package contracttest

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/controlplane/setup"
	"github.com/F31/liteAIG/internal/identity"
	"github.com/F31/liteAIG/internal/tenancy"
)

func Run(t *testing.T, db *sql.DB, repository setup.Repository) {
	t.Helper()
	ctx := context.Background()

	t.Run("rolls back partial bootstrap", func(t *testing.T) {
		state := initialState("3", "invalid")
		if err := repository.CreateInitial(ctx, state); err == nil {
			t.Fatal("CreateInitial() accepted invalid project status")
		}
		assertCount(t, db, "local_admins", 0)
		assertCount(t, db, "tenants", 0)
		assertCount(t, db, "projects", 0)
	})

	state := initialState("4", "active")
	if err := repository.CreateInitial(ctx, state); err != nil {
		t.Fatalf("CreateInitial() error = %v", err)
	}
	assertCount(t, db, "local_admins", 1)
	assertCount(t, db, "tenants", 1)
	assertCount(t, db, "projects", 1)

	var passwordHash, defaultProjectID string
	if err := db.QueryRow("SELECT password_hash FROM local_admins WHERE id = $1", state.Admin.ID).Scan(&passwordHash); err != nil {
		t.Fatal(err)
	}
	if passwordHash != state.Admin.PasswordHash || strings.Contains(passwordHash, "not-stored-password") {
		t.Fatalf("stored password hash = %q", passwordHash)
	}
	if err := db.QueryRow("SELECT default_project_id FROM tenants WHERE id = $1", state.Tenant.ID).Scan(&defaultProjectID); err != nil {
		t.Fatal(err)
	}
	if defaultProjectID != state.Project.ID {
		t.Fatalf("default_project_id = %q, want %q", defaultProjectID, state.Project.ID)
	}

	if err := repository.CreateInitial(ctx, initialState("5", "active")); !errors.Is(err, setup.ErrAlreadyInitialized) {
		t.Fatalf("second CreateInitial() error = %v", err)
	}
	assertCount(t, db, "local_admins", 1)
	assertCount(t, db, "tenants", 1)
	assertCount(t, db, "projects", 1)
}

func initialState(suffix, projectStatus string) setup.InitialState {
	now := time.Unix(10, 0)
	adminID := "00000000-0000-4000-8000-00000000000" + suffix
	tenantID := "10000000-0000-4000-8000-00000000000" + suffix
	projectID := "20000000-0000-4000-8000-00000000000" + suffix
	return setup.InitialState{
		Admin: identity.LocalAdmin{
			ID:           adminID,
			Username:     "admin-" + suffix,
			PasswordHash: "$argon2id$fixture-hash-" + suffix,
			Status:       "active",
			CreatedAt:    now,
		},
		Tenant: tenancy.Tenant{
			ID:                 tenantID,
			PublicRef:          "tenant-ref-" + suffix,
			Name:               "Tenant " + suffix,
			Status:             "active",
			SettlementCurrency: "USD",
			DefaultProjectID:   projectID,
			CreatedAt:          now,
		},
		Project: tenancy.Project{
			ID:                   projectID,
			TenantID:             tenantID,
			Name:                 "Default Project",
			Status:               projectStatus,
			ResidencyEnforcement: "advisory",
			CreatedAt:            now,
		},
	}
}

func assertCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", table, got, want)
	}
}
