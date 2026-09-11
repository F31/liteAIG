-- owner: platform
CREATE TABLE coordination_leases (
    scope      TEXT PRIMARY KEY,
    lease_id   TEXT NOT NULL,
    owner      TEXT NOT NULL DEFAULT 'platform',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- The sweeper/alert/migration singleton tasks probe expiry by scope; an index
-- is not needed for a primary-key lookup, but the platform keeps a small index
-- so Cleanup can prune expired leases without a full-table scan.
CREATE INDEX coordination_leases_expiry_idx ON coordination_leases(expires_at);