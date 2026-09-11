package config

import (
	"context"
	"errors"
	"time"

	"github.com/F31/liteAIG/internal/tenancy"
)

var (
	ErrDraftNotFound      = errors.New("config draft not found")
	ErrVersionNotFound    = errors.New("config version not found")
	ErrRevisionConflict   = errors.New("config draft revision conflict")
	ErrNoPublishedVersion = errors.New("no published config version to base a draft on")
)

type Repository interface {
	CreateDraft(context.Context, tenancy.TenantScope, Draft) error
	GetDraft(context.Context, tenancy.TenantScope, string) (*Draft, error)
	UpdateDraft(context.Context, tenancy.TenantScope, string, int64, TenantConfig, string) (*Draft, error)
	ListDrafts(context.Context, tenancy.TenantScope) ([]Draft, error)
	GetVersion(context.Context, tenancy.TenantScope, int64) (*Version, error)
	PublishDraft(context.Context, tenancy.TenantScope, PublishRecord) (*Version, error)
	RollbackVersion(context.Context, tenancy.TenantScope, RollbackRecord) (*Version, error)
	ListVersions(context.Context, tenancy.TenantScope) ([]Version, error)
	// ListLatestVersions returns the highest published version of every tenant.
	// It is a cross-tenant bootstrap query used at process startup to
	// re-activate runtime snapshots; it is not part of tenant-scoped traffic.
	ListLatestVersions(context.Context) ([]Version, error)
	Audit(context.Context, tenancy.TenantScope, AuditRecord) error
}

// AuditRecord is a config lifecycle audit event (published/reconciled/etc).
type AuditRecord struct {
	ID, ActorID, Action, ResourceID string
	Version                         int64
	OccurredAt                      time.Time
}

type PublishRecord struct {
	DraftID          string
	VersionID        string
	AuditID          string
	ActorID          string
	ExpectedRevision int64
	PublishedAt      time.Time
}

type RollbackRecord struct {
	SourceVersion int64
	VersionID     string
	AuditID       string
	ActorID       string
	PublishedAt   time.Time
}
