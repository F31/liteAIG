-- owner: tenancy
CREATE TABLE tenants (
    id UUID PRIMARY KEY,
    public_ref TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active'
      CHECK (status IN ('provisioning','active','suspended','deleting','deleted')),
    settlement_currency TEXT NOT NULL DEFAULT 'USD',
    default_project_id UUID NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    suspended_at TIMESTAMPTZ NULL,
    deleted_at TIMESTAMPTZ NULL
);

CREATE TABLE projects (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    name TEXT NOT NULL,
    route_policy_id UUID NULL,
    guardrail_policy_id UUID NULL,
    cache_policy_id UUID NULL,
    budget_policy_id UUID NULL,
    rate_policy_id UUID NULL,
    allowed_data_regions TEXT NULL,
    residency_enforcement TEXT NOT NULL DEFAULT 'advisory'
      CHECK (residency_enforcement IN ('advisory','strict')),
    status TEXT NOT NULL DEFAULT 'active'
      CHECK (status IN ('active','disabled','deleted')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ NULL,
    UNIQUE(tenant_id, name)
);

CREATE INDEX idx_projects_tenant ON projects(tenant_id);
