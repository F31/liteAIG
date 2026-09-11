// Package contracttest verifies every Tenant Repository adapter behaves identically.
package contracttest

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/tenancy"
)

const (
	tenantA = "11111111-1111-4111-8111-111111111111"
	tenantB = "22222222-2222-4222-8222-222222222222"
)

// Run executes scope, pagination, transaction, and negative isolation cases.
func Run(
	t *testing.T,
	db *sql.DB,
	repository tenancy.Repository,
	transactor tenancy.Transactor,
) {
	t.Helper()
	ctx := context.Background()
	seedTenants(t, db)

	t.Run("requires scope", func(t *testing.T) {
		if _, err := repository.GetTenant(ctx, tenancy.TenantScope{}); !errors.Is(err, tenancy.ErrInvalidScope) {
			t.Fatalf("GetTenant() error = %v", err)
		}
	})

	t.Run("gets scoped tenant", func(t *testing.T) {
		got, err := repository.GetTenant(ctx, tenancy.TenantScope{TenantID: tenantA})
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != tenantA || got.Name != "Tenant A" {
			t.Fatalf("GetTenant() = %+v", got)
		}
	})

	projects := []tenancy.Project{
		project("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa1", tenantA, "Alpha", time.Unix(1, 0)),
		project("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa2", tenantA, "Beta", time.Unix(2, 0)),
		project("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa3", tenantA, "Gamma", time.Unix(3, 0)),
		project("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbb1", tenantB, "Other", time.Unix(1, 0)),
	}
	for _, item := range projects {
		if err := repository.CreateProject(ctx, tenancy.TenantScope{TenantID: item.TenantID}, item); err != nil {
			t.Fatalf("CreateProject(%q) error = %v", item.Name, err)
		}
	}

	t.Run("rejects scope mismatch", func(t *testing.T) {
		err := repository.CreateProject(
			ctx,
			tenancy.TenantScope{TenantID: tenantA},
			project("cccccccc-cccc-4ccc-8ccc-ccccccccccc1", tenantB, "Mismatch", time.Unix(4, 0)),
		)
		if !errors.Is(err, tenancy.ErrScopeMismatch) {
			t.Fatalf("CreateProject() error = %v", err)
		}
	})

	t.Run("does not disclose cross-tenant project", func(t *testing.T) {
		_, err := repository.GetProject(
			ctx,
			tenancy.TenantScope{TenantID: tenantA},
			"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbb1",
		)
		if !errors.Is(err, tenancy.ErrNotFound) {
			t.Fatalf("GetProject() error = %v", err)
		}
	})

	t.Run("paginates within tenant", func(t *testing.T) {
		first, err := repository.ListProjects(
			ctx,
			tenancy.TenantScope{TenantID: tenantA},
			tenancy.PageRequest{Limit: 2},
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(first.Items) != 2 || first.NextOffset == nil || *first.NextOffset != 2 {
			t.Fatalf("first page = %+v", first)
		}
		second, err := repository.ListProjects(
			ctx,
			tenancy.TenantScope{TenantID: tenantA},
			tenancy.PageRequest{Offset: *first.NextOffset, Limit: 2},
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(second.Items) != 1 || second.NextOffset != nil {
			t.Fatalf("second page = %+v", second)
		}
	})

	t.Run("rolls back transaction", func(t *testing.T) {
		rollback := errors.New("rollback fixture")
		id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa4"
		err := transactor.WithinTransaction(ctx, func(tx tenancy.Repository) error {
			if err := tx.CreateProject(
				ctx,
				tenancy.TenantScope{TenantID: tenantA},
				project(id, tenantA, "Rollback", time.Unix(4, 0)),
			); err != nil {
				return err
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatalf("WithinTransaction() error = %v", err)
		}
		if _, err := repository.GetProject(ctx, tenancy.TenantScope{TenantID: tenantA}, id); !errors.Is(err, tenancy.ErrNotFound) {
			t.Fatalf("rolled-back project lookup error = %v", err)
		}
	})

	t.Run("commits transaction", func(t *testing.T) {
		id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa5"
		err := transactor.WithinTransaction(ctx, func(tx tenancy.Repository) error {
			return tx.CreateProject(
				ctx,
				tenancy.TenantScope{TenantID: tenantA},
				project(id, tenantA, "Committed", time.Unix(5, 0)),
			)
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repository.GetProject(ctx, tenancy.TenantScope{TenantID: tenantA}, id); err != nil {
			t.Fatalf("committed project lookup error = %v", err)
		}
	})
}

func seedTenants(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, tenant := range []struct {
		id, ref, name string
	}{
		{tenantA, "ref-a", "Tenant A"},
		{tenantB, "ref-b", "Tenant B"},
	} {
		if _, err := db.Exec(`
INSERT INTO tenants(id, public_ref, name, status, settlement_currency, created_at)
VALUES ($1, $2, $3, 'active', 'USD', $4)`, tenant.id, tenant.ref, tenant.name, time.Unix(0, 0)); err != nil {
			t.Fatalf("seed tenant %q: %v", tenant.name, err)
		}
	}
}

func project(id, tenantID, name string, createdAt time.Time) tenancy.Project {
	return tenancy.Project{
		ID:                   id,
		TenantID:             tenantID,
		Name:                 name,
		Status:               "active",
		ResidencyEnforcement: "advisory",
		AllowedDataRegions:   []string{"us"},
		CreatedAt:            createdAt,
	}
}
