-- owner: routing
CREATE TABLE file_mappings (
    file_id TEXT PRIMARY KEY,
    logical_model TEXT NOT NULL,
    source TEXT NOT NULL,
    source_batch_id TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_file_mappings_source_batch ON file_mappings(source_batch_id);
