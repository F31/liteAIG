-- owner: routing
CREATE TABLE batch_mappings (
    batch_id TEXT PRIMARY KEY,
    logical_model TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
