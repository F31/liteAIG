package sqlrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type BatchMappingStore struct{ db *sql.DB }

func NewBatchMappingStore(db *sql.DB) *BatchMappingStore { return &BatchMappingStore{db: db} }

func (s *BatchMappingStore) SaveBatchMapping(ctx context.Context, batchID, model string) error {
	if batchID == "" || model == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO batch_mappings(batch_id, logical_model) VALUES ($1, $2)
ON CONFLICT(batch_id) DO UPDATE SET logical_model=excluded.logical_model`, batchID, model)
	if err != nil {
		return fmt.Errorf("save batch mapping: %w", err)
	}
	return nil
}

func (s *BatchMappingStore) ModelForBatch(ctx context.Context, batchID string) (string, error) {
	var model string
	err := s.db.QueryRowContext(ctx, `SELECT logical_model FROM batch_mappings WHERE batch_id=$1`, batchID).Scan(&model)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("batch mapping not found")
	}
	if err != nil {
		return "", fmt.Errorf("lookup batch mapping: %w", err)
	}
	return model, nil
}

func (s *BatchMappingStore) SaveBatchFileMapping(ctx context.Context, batchID, fileID, model string) error {
	return s.SaveFileMapping(ctx, fileID, model, "batch_result", batchID)
}

func (s *BatchMappingStore) ModelForBatchFile(ctx context.Context, fileID string) (string, error) {
	return s.ModelForFile(ctx, fileID)
}

func (s *BatchMappingStore) SaveFileMapping(ctx context.Context, fileID, model, source, sourceBatchID string) error {
	if fileID == "" || model == "" || source == "" {
		return nil
	}
	if source == "batch_result" && sourceBatchID == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO file_mappings(file_id, logical_model, source, source_batch_id) VALUES ($1, $2, $3, $4)
ON CONFLICT(file_id) DO UPDATE SET logical_model=excluded.logical_model, source=excluded.source, source_batch_id=excluded.source_batch_id`, fileID, model, source, nullString(sourceBatchID))
	if err != nil {
		return fmt.Errorf("save file mapping: %w", err)
	}
	return nil
}

func (s *BatchMappingStore) ModelForFile(ctx context.Context, fileID string) (string, error) {
	var model string
	err := s.db.QueryRowContext(ctx, `SELECT logical_model FROM file_mappings WHERE file_id=$1`, fileID).Scan(&model)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("file mapping not found")
	}
	if err != nil {
		return "", fmt.Errorf("lookup file mapping: %w", err)
	}
	return model, nil
}

func (s *BatchMappingStore) DeleteFileMapping(ctx context.Context, fileID string) error {
	if fileID == "" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM file_mappings WHERE file_id=$1`, fileID); err != nil {
		return fmt.Errorf("delete file mapping: %w", err)
	}
	return nil
}

func (s *BatchMappingStore) PurgeFileMappingsOlderThan(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM file_mappings WHERE created_at < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("purge file mappings: %w", err)
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("file mapping purge rows affected: %w", err)
	}
	return removed, nil
}
