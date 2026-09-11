-- owner: observability
CREATE TABLE audit_retention (
    tenant_id UUID PRIMARY KEY REFERENCES tenants(id),
    retention_days INTEGER NOT NULL CHECK (retention_days > 0),
    updated_by TEXT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);