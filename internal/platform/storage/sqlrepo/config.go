package sqlrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	controlconfig "github.com/F31/liteAIG/internal/controlplane/config"
	"github.com/F31/liteAIG/internal/tenancy"
)

type ConfigRepository struct{ db *sql.DB }

func NewConfigRepository(db *sql.DB) *ConfigRepository { return &ConfigRepository{db: db} }

func (r *ConfigRepository) CreateDraft(ctx context.Context, scope tenancy.TenantScope, draft controlconfig.Draft) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if draft.TenantID != scope.TenantID {
		return tenancy.ErrScopeMismatch
	}
	data, err := json.Marshal(draft.Config)
	if err != nil {
		return fmt.Errorf("encode config draft: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO config_drafts(
    id, scope_type, tenant_id, base_version, revision, status, changes,
    source_type, created_by, updated_by, created_at, updated_at
) VALUES ($1, 'tenant', $2, $3, $4, $5, $6, 'manual', $7, $8, $9, $10)`,
		draft.ID, draft.TenantID, draft.BaseVersion, draft.Revision, draft.Status,
		string(data), draft.CreatedBy, draft.UpdatedBy, draft.CreatedAt, draft.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create config draft: %w", err)
	}
	return nil
}

func (r *ConfigRepository) GetDraft(ctx context.Context, scope tenancy.TenantScope, id string) (*controlconfig.Draft, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	var draft controlconfig.Draft
	var encoded string
	var createdAt, updatedAt databaseTime
	err := r.db.QueryRowContext(ctx, `
SELECT id, tenant_id, base_version, revision, status, changes,
       created_by, updated_by, created_at, updated_at
FROM config_drafts
WHERE tenant_id = $1 AND id = $2`, scope.TenantID, id).Scan(
		&draft.ID, &draft.TenantID, &draft.BaseVersion, &draft.Revision, &draft.Status,
		&encoded, &draft.CreatedBy, &draft.UpdatedBy, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, controlconfig.ErrDraftNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get config draft: %w", err)
	}
	if err := json.Unmarshal([]byte(encoded), &draft.Config); err != nil {
		return nil, fmt.Errorf("decode config draft: %w", err)
	}
	draft.CreatedAt, draft.UpdatedAt = createdAt.Time, updatedAt.Time
	return &draft, nil
}

// ListDrafts returns the tenant's config drafts in updated order.
func (r *ConfigRepository) ListDrafts(ctx context.Context, scope tenancy.TenantScope) ([]controlconfig.Draft, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id, tenant_id, base_version, revision, status, changes,
       created_by, updated_by, created_at, updated_at
FROM config_drafts
WHERE tenant_id = $1
ORDER BY updated_at DESC`, scope.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []controlconfig.Draft
	for rows.Next() {
		var draft controlconfig.Draft
		var encoded string
		var createdAt, updatedAt databaseTime
		if err := rows.Scan(&draft.ID, &draft.TenantID, &draft.BaseVersion, &draft.Revision, &draft.Status,
			&encoded, &draft.CreatedBy, &draft.UpdatedBy, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(encoded), &draft.Config); err != nil {
			return nil, fmt.Errorf("decode config draft %s: %w", draft.ID, err)
		}
		draft.CreatedAt, draft.UpdatedAt = createdAt.Time, updatedAt.Time
		result = append(result, draft)
	}
	return result, rows.Err()
}

func (r *ConfigRepository) UpdateDraft(
	ctx context.Context,
	scope tenancy.TenantScope,
	id string,
	expectedRevision int64,
	document controlconfig.TenantConfig,
	actorID string,
) (*controlconfig.Draft, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode config draft: %w", err)
	}
	result, err := r.db.ExecContext(ctx, `
UPDATE config_drafts
SET changes = $1, revision = revision + 1, updated_by = $2, updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = $3 AND id = $4 AND revision = $5 AND status = 'editing'`,
		string(data), actorID, scope.TenantID, id, expectedRevision,
	)
	if err != nil {
		return nil, fmt.Errorf("update config draft: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("update config draft result: %w", err)
	}
	if rows != 1 {
		return nil, controlconfig.ErrRevisionConflict
	}
	return r.GetDraft(ctx, scope, id)
}

func (r *ConfigRepository) GetVersion(ctx context.Context, scope tenancy.TenantScope, version int64) (*controlconfig.Version, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	var id, encoded, actor string
	var source sql.NullString
	var published databaseTime
	err := r.db.QueryRowContext(ctx, `SELECT id, source_draft_id, compiled_config, published_by, published_at FROM config_versions WHERE tenant_id=$1 AND version=$2`, scope.TenantID, version).Scan(&id, &source, &encoded, &actor, &published)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, controlconfig.ErrVersionNotFound
	}
	if err != nil {
		return nil, err
	}
	return decodeVersion(id, scope.TenantID, version, source.String, encoded, actor, published.Time)
}

func (r *ConfigRepository) PublishDraft(ctx context.Context, scope tenancy.TenantScope, record controlconfig.PublishRecord) (*controlconfig.Version, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var encoded string
	result, err := tx.ExecContext(ctx, `UPDATE config_drafts SET status='published', updated_by=$1, updated_at=$2 WHERE tenant_id=$3 AND id=$4 AND revision=$5 AND status='editing'`, record.ActorID, record.PublishedAt, scope.TenantID, record.DraftID, record.ExpectedRevision)
	if err != nil {
		return nil, fmt.Errorf("claim config draft: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return nil, controlconfig.ErrRevisionConflict
	}
	if err := tx.QueryRowContext(ctx, `SELECT changes FROM config_drafts WHERE tenant_id=$1 AND id=$2`, scope.TenantID, record.DraftID).Scan(&encoded); err != nil {
		return nil, err
	}
	var next int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM config_versions WHERE tenant_id=$1`, scope.TenantID).Scan(&next); err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO config_versions(id, scope_type, tenant_id, version, source_draft_id, compiled_config, published_by, published_at) VALUES ($1,'tenant',$2,$3,$4,$5,$6,$7)`, record.VersionID, scope.TenantID, next, record.DraftID, encoded, record.ActorID, record.PublishedAt)
	if err != nil {
		return nil, fmt.Errorf("create config version: %w", err)
	}
	if err := insertConfigAudit(ctx, tx, record.AuditID, scope.TenantID, record.ActorID, "config.publish", record.VersionID, next, record.PublishedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return decodeVersion(record.VersionID, scope.TenantID, next, record.DraftID, encoded, record.ActorID, record.PublishedAt)
}

func (r *ConfigRepository) RollbackVersion(ctx context.Context, scope tenancy.TenantScope, record controlconfig.RollbackRecord) (*controlconfig.Version, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var encoded string
	if err := tx.QueryRowContext(ctx, `SELECT compiled_config FROM config_versions WHERE tenant_id=$1 AND version=$2`, scope.TenantID, record.SourceVersion).Scan(&encoded); errors.Is(err, sql.ErrNoRows) {
		return nil, controlconfig.ErrVersionNotFound
	} else if err != nil {
		return nil, err
	}
	var next int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM config_versions WHERE tenant_id=$1`, scope.TenantID).Scan(&next); err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO config_versions(id, scope_type, tenant_id, version, source_draft_id, compiled_config, published_by, published_at) VALUES ($1,'tenant',$2,$3,NULL,$4,$5,$6)`, record.VersionID, scope.TenantID, next, encoded, record.ActorID, record.PublishedAt)
	if err != nil {
		return nil, err
	}
	if err := insertConfigAudit(ctx, tx, record.AuditID, scope.TenantID, record.ActorID, "config.rollback", record.VersionID, record.SourceVersion, record.PublishedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return decodeVersion(record.VersionID, scope.TenantID, next, "", encoded, record.ActorID, record.PublishedAt)
}

func insertConfigAudit(ctx context.Context, tx *sql.Tx, id, tenantID, actorID, action, resourceID string, version int64, occurredAt time.Time) error {
	details, _ := json.Marshal(map[string]int64{"version": version})
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_events(id, tenant_id, scope, actor_id, action, resource_type, resource_id, result, details, occurred_at) VALUES ($1,$2,'tenant',$3,$4,'config_version',$5,'success',$6,$7)`, id, tenantID, actorID, action, resourceID, string(details), occurredAt)
	if err != nil {
		return fmt.Errorf("write config audit: %w", err)
	}
	return nil
}

// Audit writes a config lifecycle audit event outside a transaction.
func (r *ConfigRepository) Audit(ctx context.Context, scope tenancy.TenantScope, record controlconfig.AuditRecord) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	details, _ := json.Marshal(map[string]int64{"version": record.Version})
	_, err := r.db.ExecContext(ctx, `INSERT INTO audit_events(id, tenant_id, scope, actor_id, action, resource_type, resource_id, result, details, occurred_at) VALUES ($1,$2,'tenant',$3,$4,'config_version',$5,'success',$6,$7)`, record.ID, scope.TenantID, record.ActorID, record.Action, record.ResourceID, string(details), record.OccurredAt)
	if err != nil {
		return fmt.Errorf("write config audit: %w", err)
	}
	return nil
}

// ListVersions returns all published versions for a tenant in ascending order.
func (r *ConfigRepository) ListVersions(ctx context.Context, scope tenancy.TenantScope) ([]controlconfig.Version, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, tenant_id, version, source_draft_id, compiled_config, published_by, published_at FROM config_versions WHERE tenant_id=$1 ORDER BY version`, scope.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []controlconfig.Version
	for rows.Next() {
		var id, tenantID, actor string
		var version int64
		var source sql.NullString
		var encoded string
		var published databaseTime
		if err := rows.Scan(&id, &tenantID, &version, &source, &encoded, &actor, &published); err != nil {
			return nil, err
		}
		item, err := decodeVersion(id, tenantID, version, source.String, encoded, actor, published.Time)
		if err != nil {
			return nil, err
		}
		result = append(result, *item)
	}
	return result, rows.Err()
}

// ListLatestVersions returns the highest published version per tenant across
// all tenants. It is used by the startup reconcile to rebuild runtime
// snapshots after a process restart.
func (r *ConfigRepository) ListLatestVersions(ctx context.Context) ([]controlconfig.Version, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT cv.id, cv.tenant_id, cv.version, cv.source_draft_id, cv.compiled_config,
       cv.published_by, cv.published_at
FROM config_versions cv
JOIN (
    SELECT tenant_id, MAX(version) AS max_version
    FROM config_versions
    GROUP BY tenant_id
) latest ON latest.tenant_id = cv.tenant_id AND latest.max_version = cv.version
ORDER BY cv.tenant_id, cv.version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []controlconfig.Version
	for rows.Next() {
		var id, tenantID, actor string
		var version int64
		var source sql.NullString
		var encoded string
		var published databaseTime
		if err := rows.Scan(&id, &tenantID, &version, &source, &encoded, &actor, &published); err != nil {
			return nil, err
		}
		item, err := decodeVersion(id, tenantID, version, source.String, encoded, actor, published.Time)
		if err != nil {
			return nil, err
		}
		result = append(result, *item)
	}
	return result, rows.Err()
}

func decodeVersion(id, tenantID string, version int64, sourceDraftID, encoded, actor string, publishedAt time.Time) (*controlconfig.Version, error) {
	var document controlconfig.TenantConfig
	if err := json.Unmarshal([]byte(encoded), &document); err != nil {
		return nil, err
	}
	return &controlconfig.Version{ID: id, TenantID: tenantID, Version: version, SourceDraftID: sourceDraftID, Config: document, PublishedBy: actor, PublishedAt: publishedAt}, nil
}

var _ controlconfig.Repository = (*ConfigRepository)(nil)
