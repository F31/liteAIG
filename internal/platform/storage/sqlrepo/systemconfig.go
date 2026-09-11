package sqlrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	controlconfig "github.com/F31/liteAIG/internal/controlplane/config"
)

// SystemConfigRepository persists the singleton system-level configuration
// (A4 tenant policy defaults). The row carries no tenant scope; reads and
// writes are restricted to the system_admin-gated Admin API surface.
type SystemConfigRepository struct{ db *sql.DB }

func NewSystemConfigRepository(db *sql.DB) *SystemConfigRepository {
	return &SystemConfigRepository{db: db}
}

// GetSystemConfig returns the persisted system config, or an empty config
// when the singleton row has not been created yet.
func (r *SystemConfigRepository) GetSystemConfig(ctx context.Context) (controlconfig.SystemConfig, error) {
	var encoded string
	err := r.db.QueryRowContext(ctx, `SELECT config FROM system_config WHERE id = 1`).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return controlconfig.SystemConfig{}, nil
	}
	if err != nil {
		return controlconfig.SystemConfig{}, fmt.Errorf("read system config: %w", err)
	}
	var config controlconfig.SystemConfig
	if err := json.Unmarshal([]byte(encoded), &config); err != nil {
		return controlconfig.SystemConfig{}, fmt.Errorf("decode system config: %w", err)
	}
	return config, nil
}

// SetSystemConfig upserts the singleton system config row.
func (r *SystemConfigRepository) SetSystemConfig(ctx context.Context, document controlconfig.SystemConfig, actorID string) (controlconfig.SystemConfig, error) {
	data, err := json.Marshal(document)
	if err != nil {
		return controlconfig.SystemConfig{}, fmt.Errorf("encode system config: %w", err)
	}
	if _, err := r.db.ExecContext(ctx, `
INSERT INTO system_config(id, config, updated_by, updated_at)
VALUES (1, $1, $2, CURRENT_TIMESTAMP)
ON CONFLICT (id) DO UPDATE SET config = $1, updated_by = $2, updated_at = CURRENT_TIMESTAMP`,
		string(data), actorID); err != nil {
		return controlconfig.SystemConfig{}, fmt.Errorf("write system config: %w", err)
	}
	return document, nil
}
