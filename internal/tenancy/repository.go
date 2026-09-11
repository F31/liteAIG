package tenancy

import "context"

// Repository exposes only tenant-scoped operations for existing resources.
type Repository interface {
	GetTenant(context.Context, TenantScope) (*Tenant, error)
	CreateProject(context.Context, TenantScope, Project) error
	GetProject(context.Context, TenantScope, string) (*Project, error)
	ListProjects(context.Context, TenantScope, PageRequest) (ProjectPage, error)
}

// Transactor runs tenant repository operations in one database transaction.
type Transactor interface {
	WithinTransaction(context.Context, func(Repository) error) error
}

// TenantLister covers unscoped bootstrap reads (the console setup-status
// probe) that no domain Repository method expresses.
type TenantLister interface {
	FirstTenant(context.Context) (*Tenant, error)
}

// TenantAdmin covers unscoped system-operator tenant lifecycle operations.
// Tenant-scoped data access remains on Repository and must still validate
// TenantScope before reading or mutating tenant-owned resources.
type TenantAdmin interface {
	ListTenants(context.Context, PageRequest) (TenantPage, error)
	CreateTenantWithDefaultProject(context.Context, Tenant, Project) error
	UpdateTenantStatus(context.Context, string, string) error
}
