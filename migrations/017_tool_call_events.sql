-- owner: observability
CREATE TABLE tool_call_events (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    project_id UUID NULL,
    request_id UUID NULL,
    session_id TEXT NULL,
    task_id TEXT NULL,
    agent_id TEXT NULL,
    user_id TEXT NULL,
    tool_id TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_tool_call_events_tenant_time ON tool_call_events(tenant_id, occurred_at DESC);
