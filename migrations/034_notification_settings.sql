-- owner: observability
CREATE TABLE notification_settings (
    tenant_id UUID PRIMARY KEY REFERENCES tenants(id),
    webhook_url TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    updated_by TEXT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
