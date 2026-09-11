package contracttest

import (
	"context"
	"errors"
	"testing"

	"github.com/F31/liteAIG/internal/tenancy"
)

// RunTenantAdmin exercises the system-operator tenant lifecycle operations
// that a self-hosted control plane exposes over the Admin API.
func RunTenantAdmin(t *testing.T, repository tenancy.TenantAdmin) {
	t.Helper()
	ctx := context.Background()

	t.Run("lists tenants in stable order", func(t *testing.T) {
		page, err := repository.ListTenants(ctx, tenancy.PageRequest{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) < 2 {
			t.Fatalf("ListTenants() = %d items, want >= 2", len(page.Items))
		}
		seen := map[string]bool{}
		for _, item := range page.Items {
			if item.ID == "" || item.PublicRef == "" || item.Name == "" {
				t.Fatalf("ListTenants() returned incomplete tenant: %+v", item)
			}
			if seen[item.ID] {
				t.Fatalf("ListTenants() returned duplicate tenant id %q", item.ID)
			}
			seen[item.ID] = true
		}
		if page.NextOffset != nil {
			t.Fatalf("ListTenants() NextOffset = %v, want nil for full page", *page.NextOffset)
		}
	})

	t.Run("paginates tenants", func(t *testing.T) {
		first, err := repository.ListTenants(ctx, tenancy.PageRequest{Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(first.Items) != 1 || first.NextOffset == nil {
			t.Fatalf("first page = %+v", first)
		}
		second, err := repository.ListTenants(ctx, tenancy.PageRequest{Offset: *first.NextOffset, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(second.Items) != 1 {
			t.Fatalf("second page = %+v", second)
		}
		if first.Items[0].ID == second.Items[0].ID {
			t.Fatalf("pages overlap: %q in both pages", first.Items[0].ID)
		}
	})

	t.Run("creates tenant with default project", func(t *testing.T) {
		tenant := tenancy.Tenant{
			ID:        "33333333-3333-4333-8333-333333333333",
			PublicRef: "ref-c",
			Name:      "Tenant C",
		}
		project := tenancy.Project{
			ID:       "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa11",
			TenantID: tenant.ID,
			Name:     "Default",
		}
		if err := repository.CreateTenantWithDefaultProject(ctx, tenant, project); err != nil {
			t.Fatal(err)
		}
		lister, ok := repository.(tenancy.Repository)
		if ok {
			got, err := lister.GetTenant(ctx, tenancy.TenantScope{TenantID: tenant.ID})
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != "active" || got.DefaultProjectID != project.ID {
				t.Fatalf("GetTenant() = %+v", got)
			}
			if _, err := lister.GetProject(ctx, tenancy.TenantScope{TenantID: tenant.ID}, project.ID); err != nil {
				t.Fatalf("default project missing: %v", err)
			}
		}
	})

	t.Run("rejects malformed create", func(t *testing.T) {
		if err := repository.CreateTenantWithDefaultProject(ctx, tenancy.Tenant{}, tenancy.Project{}); err == nil {
			t.Fatal("CreateTenantWithDefaultProject() error = nil, want error")
		}
	})

	t.Run("updates tenant status", func(t *testing.T) {
		if err := repository.UpdateTenantStatus(ctx, tenantB, "suspended"); err != nil {
			t.Fatal(err)
		}
		lister, ok := repository.(tenancy.Repository)
		if ok {
			got, err := lister.GetTenant(ctx, tenancy.TenantScope{TenantID: tenantB})
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != "suspended" {
				t.Fatalf("GetTenant() status = %q, want suspended", got.Status)
			}
		}
		if err := repository.UpdateTenantStatus(ctx, tenantB, "active"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("rejects unsupported status", func(t *testing.T) {
		if err := repository.UpdateTenantStatus(ctx, tenantA, "flying"); err == nil {
			t.Fatal("UpdateTenantStatus() error = nil, want error")
		}
	})

	t.Run("missing tenant reports not found", func(t *testing.T) {
		if err := repository.UpdateTenantStatus(ctx, "99999999-9999-4999-8999-999999999999", "suspended"); !errors.Is(err, tenancy.ErrNotFound) {
			t.Fatalf("UpdateTenantStatus() error = %v, want %v", err, tenancy.ErrNotFound)
		}
	})
}
