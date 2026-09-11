-- owner: observability
CREATE TABLE request_records (
    request_id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    project_id UUID NOT NULL REFERENCES projects(id),
    key_id UUID NULL,
    logical_model TEXT NOT NULL,
    deployment_id UUID NULL,
    outcome TEXT NOT NULL,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    retry_count INTEGER NOT NULL DEFAULT 0,
    fallback_count INTEGER NOT NULL DEFAULT 0,
    route_evidence TEXT NOT NULL DEFAULT '[]',
    attempts TEXT NOT NULL DEFAULT '[]',
    guardrail_status TEXT NULL,
    provider_cost NUMERIC(18,9) NULL,
    provider_currency TEXT NULL,
    source TEXT NOT NULL DEFAULT 'api',
    latency_ms BIGINT NOT NULL DEFAULT 0,
    tenant_snapshot_version BIGINT NOT NULL,
    security_epoch BIGINT NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NULL
);

CREATE INDEX idx_request_records_tenant_received
  ON request_records(tenant_id, received_at);

CREATE TABLE audit_events (
    id UUID PRIMARY KEY,
    tenant_id UUID NULL REFERENCES tenants(id),
    scope TEXT NOT NULL CHECK (scope IN ('system','tenant')),
    actor_id UUID NOT NULL,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NULL,
    result TEXT NOT NULL,
    details TEXT NOT NULL DEFAULT '{}',
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_audit_events_tenant_occurred
  ON audit_events(tenant_id, occurred_at);
