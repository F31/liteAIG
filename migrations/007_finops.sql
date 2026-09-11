-- owner: finops
CREATE TABLE usage_events (
    id UUID PRIMARY KEY,
    request_id UUID NOT NULL,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    project_id UUID NOT NULL REFERENCES projects(id),
    key_id UUID NULL,
    logical_model TEXT NOT NULL,
    provider_id UUID NULL,
    deployment_id UUID NULL,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    total_tokens BIGINT NOT NULL DEFAULT 0,
    usage_source TEXT NOT NULL,
    estimated BOOLEAN NOT NULL DEFAULT false,
    provider_cost NUMERIC(18,9) NULL,
    provider_currency TEXT NULL,
    pricing_source TEXT NULL,
    tenant_snapshot_version BIGINT NOT NULL,
    security_epoch BIGINT NOT NULL,
    status TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(tenant_id, request_id)
);

CREATE INDEX idx_usage_events_tenant_occurred
  ON usage_events(tenant_id, occurred_at);

CREATE TABLE budget_state (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    project_id UUID NULL REFERENCES projects(id),
    key_id UUID NULL,
    window_start TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    token_limit BIGINT NOT NULL,
    reserved_tokens BIGINT NOT NULL DEFAULT 0,
    consumed_tokens BIGINT NOT NULL DEFAULT 0,
    mode TEXT NOT NULL CHECK (mode IN ('hard','soft')),
    version BIGINT NOT NULL DEFAULT 1
);

CREATE INDEX idx_budget_state_tenant_window
  ON budget_state(tenant_id, window_start, window_end);
