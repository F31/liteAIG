-- owner: catalog
CREATE TABLE providers (
    id UUID PRIMARY KEY,
    tenant_id UUID NULL REFERENCES tenants(id),
    owner_scope TEXT NOT NULL
      CHECK (owner_scope IN ('SYSTEM_SHARED','TENANT_PRIVATE')),
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    config TEXT NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'enabled',
    CHECK (
      (owner_scope = 'SYSTEM_SHARED' AND tenant_id IS NULL)
      OR
      (owner_scope = 'TENANT_PRIVATE' AND tenant_id IS NOT NULL)
    )
);

CREATE TABLE provider_credentials (
    id UUID PRIMARY KEY,
    provider_id UUID NOT NULL REFERENCES providers(id),
    tenant_id UUID NULL REFERENCES tenants(id),
    owner_scope TEXT NOT NULL
      CHECK (owner_scope IN ('SYSTEM_SHARED','TENANT_PRIVATE')),
    label TEXT NOT NULL,
    secret_ref TEXT NOT NULL,
    weight INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL DEFAULT 'enabled',
    fingerprint TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (
      (owner_scope = 'SYSTEM_SHARED' AND tenant_id IS NULL)
      OR
      (owner_scope = 'TENANT_PRIVATE' AND tenant_id IS NOT NULL)
    )
);

CREATE TABLE model_deployments (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    provider_id UUID NOT NULL REFERENCES providers(id),
    credential_id UUID NOT NULL REFERENCES provider_credentials(id),
    upstream_model TEXT NOT NULL,
    endpoint TEXT NULL,
    region TEXT NULL,
    data_region TEXT NOT NULL DEFAULT 'unspecified',
    capabilities TEXT NOT NULL DEFAULT '{}',
    context_window INTEGER NULL,
    max_output_tokens INTEGER NULL,
    priority INTEGER NOT NULL DEFAULT 0,
    capacity INTEGER NULL,
    status TEXT NOT NULL DEFAULT 'enabled'
);

CREATE INDEX idx_model_deployments_tenant ON model_deployments(tenant_id, status);

CREATE TABLE logical_models (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    alias TEXT NOT NULL,
    route_policy_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(tenant_id, alias)
);
