-- owner: controlplane
-- Singleton system-level configuration (A4 SaaS policy defaults). A single row
-- (id=1) holds the SystemConfig document consumed by the tenant compile path.
CREATE TABLE system_config (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    config TEXT NOT NULL,
    updated_by TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
