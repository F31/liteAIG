// Package organization owns Users, OrgUnits, effective-dated memberships, and
// cost-center attribution.
package organization

import (
	"context"
	"time"

	"github.com/F31/liteAIG/internal/tenancy"
)

type User struct {
	ID          string
	DisplayName string
	Email       string
	Status      string
	CreatedAt   time.Time
}

type OrgUnit struct {
	ID           string
	TenantID     string
	ParentID     string
	Name         string
	Code         string
	CostCenterID string
	Path         string
	Status       string
	CreatedAt    time.Time
}

// UserOrgAssignment is an effective-dated membership. Personnel moves close the
// previous interval and open a new one; history is never rewritten.
type UserOrgAssignment struct {
	ID        string
	TenantID  string
	UserID    string
	OrgUnitID string
	IsPrimary bool
	ValidFrom time.Time
	ValidTo   *time.Time
	CreatedAt time.Time
}

// PrincipalAttribution is the snapshot written into usage/request facts.
type PrincipalAttribution struct {
	UserID           string
	OrgUnitID        string
	OrgPath          string
	CostCenterID     string
	AttributionTrust string
}

// Repository exposes tenant-scoped organization reads and effective-dated writes.
type Repository interface {
	GetUser(context.Context, tenancy.TenantScope, string) (*User, error)
	GetOrgUnit(context.Context, tenancy.TenantScope, string) (*OrgUnit, error)
	ActiveAssignment(context.Context, tenancy.TenantScope, string, time.Time) (*UserOrgAssignment, error)
	ListAssignments(context.Context, tenancy.TenantScope, string) ([]UserOrgAssignment, error)
	CreateOrgUnit(context.Context, tenancy.TenantScope, OrgUnit) error
	AssignUser(context.Context, tenancy.TenantScope, UserOrgAssignment) error
}
