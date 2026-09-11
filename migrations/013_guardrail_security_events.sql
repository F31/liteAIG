-- owner: guardrail
CREATE TABLE security_events (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    project_id UUID NULL REFERENCES projects(id),
    request_id UUID NULL,
    policy_id TEXT NOT NULL,
    rule_id TEXT NOT NULL,
    action TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    snapshot_version BIGINT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_security_events_tenant_time ON security_events(tenant_id, occurred_at);
