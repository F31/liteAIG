-- owner: routing
CREATE TABLE route_policies (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    project_id UUID NULL REFERENCES projects(id),
    name TEXT NOT NULL,
    strategy TEXT NOT NULL CHECK (strategy IN ('priority','weighted','round_robin')),
    deployment_config TEXT NOT NULL DEFAULT '[]',
    version INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(tenant_id, name)
);
