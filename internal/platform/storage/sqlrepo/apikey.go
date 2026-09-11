package sqlrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/F31/liteAIG/internal/identity/apikey"
	"github.com/F31/liteAIG/internal/tenancy"
)

type APIKeyRepository struct{ db *sql.DB }

func NewAPIKeyRepository(db *sql.DB) *APIKeyRepository { return &APIKeyRepository{db: db} }

func (r *APIKeyRepository) ActivePepper(ctx context.Context) (apikey.PepperVersion, error) {
	var value apikey.PepperVersion
	err := r.db.QueryRowContext(ctx, `SELECT version, pepper_ref, status FROM key_pepper_versions WHERE status='active' ORDER BY version DESC LIMIT 1`).Scan(&value.Version, &value.Ref, &value.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return value, errors.New("no active key pepper")
	}
	return value, err
}

func (r *APIKeyRepository) ListRuntimePeppers(ctx context.Context) ([]apikey.PepperVersion, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT version,pepper_ref,status FROM key_pepper_versions WHERE status IN ('active','retiring') ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []apikey.PepperVersion
	for rows.Next() {
		var item apikey.PepperVersion
		if err := rows.Scan(&item.Version, &item.Ref, &item.Status); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *APIKeyRepository) Create(ctx context.Context, scope tenancy.TenantScope, key apikey.Record) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if key.TenantID != scope.TenantID {
		return tenancy.ErrScopeMismatch
	}
	var projectExists int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, scope.TenantID, key.ProjectID).Scan(&projectExists); err != nil {
		return err
	}
	if projectExists != 1 {
		return tenancy.ErrScopeMismatch
	}
	models, _ := json.Marshal(key.ModelAllowlist)
	ips, _ := json.Marshal(key.IPAllowlist)
	_, err := r.db.ExecContext(ctx, `INSERT INTO api_keys(id,public_id,tenant_id,project_id,name,hmac_digest,pepper_version,fingerprint,application_id,agent_id,service_account_id,status,expires_at,model_allowlist,ip_allowlist,created_at,key_ciphertext) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, key.ID, key.PublicID, key.TenantID, key.ProjectID, key.Name, key.HMACDigest, key.PepperVersion, key.Fingerprint, nullString(key.ApplicationID), nullString(key.AgentID), nullString(key.ServiceAccountID), key.Status, key.ExpiresAt, string(models), string(ips), key.CreatedAt, nullBytes(key.KeyCiphertext))
	if err != nil {
		return fmt.Errorf("create API key: %w", err)
	}
	return nil
}

func (r *APIKeyRepository) Revoke(ctx context.Context, scope tenancy.TenantScope, id string) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `UPDATE api_keys SET status='revoked' WHERE tenant_id=$1 AND id=$2 AND status='active'`, scope.TenantID, id)
	if err != nil {
		return fmt.Errorf("revoke API key: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return tenancy.ErrScopeMismatch
	}
	return nil
}

func (r *APIKeyRepository) ListActive(ctx context.Context, scope tenancy.TenantScope) ([]apikey.Record, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,public_id,tenant_id,project_id,name,hmac_digest,pepper_version,fingerprint,application_id,agent_id,service_account_id,status,expires_at,model_allowlist,ip_allowlist,created_at FROM api_keys WHERE tenant_id=$1 AND status='active'`, scope.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []apikey.Record
	for rows.Next() {
		var item apikey.Record
		var app, agent, service, models, ips sql.NullString
		var expiry nullableDatabaseTime
		var created databaseTime
		if err := rows.Scan(&item.ID, &item.PublicID, &item.TenantID, &item.ProjectID, &item.Name, &item.HMACDigest, &item.PepperVersion, &item.Fingerprint, &app, &agent, &service, &item.Status, &expiry, &models, &ips, &created); err != nil {
			return nil, err
		}
		item.ApplicationID, item.AgentID, item.ServiceAccountID = app.String, agent.String, service.String
		item.CreatedAt = created.Time
		if expiry.Valid {
			value := expiry.Time
			item.ExpiresAt = &value
		}
		if models.Valid {
			if err := json.Unmarshal([]byte(models.String), &item.ModelAllowlist); err != nil {
				return nil, fmt.Errorf("decode model allowlist: %w", err)
			}
		}
		if ips.Valid {
			if err := json.Unmarshal([]byte(ips.String), &item.IPAllowlist); err != nil {
				return nil, fmt.Errorf("decode IP allowlist: %w", err)
			}
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *APIKeyRepository) List(ctx context.Context, scope tenancy.TenantScope) ([]apikey.Record, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,public_id,tenant_id,project_id,name,hmac_digest,pepper_version,fingerprint,application_id,agent_id,service_account_id,status,expires_at,model_allowlist,ip_allowlist,created_at,key_ciphertext FROM api_keys WHERE tenant_id=$1 ORDER BY created_at`, scope.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []apikey.Record
	for rows.Next() {
		item, err := scanKeyRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *APIKeyRepository) Get(ctx context.Context, scope tenancy.TenantScope, id string) (apikey.Record, error) {
	if err := scope.Validate(); err != nil {
		return apikey.Record{}, err
	}
	row := r.db.QueryRowContext(ctx, `SELECT id,public_id,tenant_id,project_id,name,hmac_digest,pepper_version,fingerprint,application_id,agent_id,service_account_id,status,expires_at,model_allowlist,ip_allowlist,created_at,key_ciphertext FROM api_keys WHERE tenant_id=$1 AND id=$2`, scope.TenantID, id)
	item, err := scanKeyRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return apikey.Record{}, tenancy.ErrNotFound
	}
	if err != nil {
		return apikey.Record{}, err
	}
	return item, nil
}

type scanner interface{ Scan(...any) error }

func scanKeyRow(row scanner) (apikey.Record, error) {
	var item apikey.Record
	var app, agent, service, models, ips sql.NullString
	var ciphertext []byte
	var expiry nullableDatabaseTime
	var created databaseTime
	err := row.Scan(&item.ID, &item.PublicID, &item.TenantID, &item.ProjectID, &item.Name, &item.HMACDigest, &item.PepperVersion, &item.Fingerprint, &app, &agent, &service, &item.Status, &expiry, &models, &ips, &created, &ciphertext)
	if err != nil {
		return apikey.Record{}, err
	}
	item.ApplicationID, item.AgentID, item.ServiceAccountID = app.String, agent.String, service.String
	item.CreatedAt = created.Time
	if expiry.Valid {
		value := expiry.Time
		item.ExpiresAt = &value
	}
	if models.Valid {
		if err := json.Unmarshal([]byte(models.String), &item.ModelAllowlist); err != nil {
			return apikey.Record{}, fmt.Errorf("decode model allowlist: %w", err)
		}
	}
	if ips.Valid {
		if err := json.Unmarshal([]byte(ips.String), &item.IPAllowlist); err != nil {
			return apikey.Record{}, fmt.Errorf("decode IP allowlist: %w", err)
		}
	}
	if len(ciphertext) > 0 {
		item.KeyCiphertext = ciphertext
	}
	return item, nil
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

var _ apikey.Repository = (*APIKeyRepository)(nil)
