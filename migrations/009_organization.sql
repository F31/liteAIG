-- owner: organization
CREATE TABLE users (
    id UUID PRIMARY KEY,
    display_name TEXT NOT NULL,
    email TEXT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE tenant_memberships (
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    user_id UUID NOT NULL REFERENCES users(id),
    status TEXT NOT NULL DEFAULT 'active',
    joined_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    left_at TIMESTAMPTZ NULL,
    PRIMARY KEY (tenant_id, user_id)
);

CREATE TABLE cost_centers (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    code TEXT NOT NULL,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(tenant_id, code)
);

CREATE TABLE org_units (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    parent_id UUID NULL REFERENCES org_units(id),
    name TEXT NOT NULL,
    code TEXT NULL,
    cost_center_id UUID NULL REFERENCES cost_centers(id),
    path TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(tenant_id, parent_id, name)
);
CREATE INDEX idx_org_units_tenant_parent ON org_units(tenant_id, parent_id);
CREATE INDEX idx_org_units_tenant_path ON org_units(tenant_id, path);

CREATE TABLE user_org_assignments (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    user_id UUID NOT NULL REFERENCES users(id),
    org_unit_id UUID NOT NULL REFERENCES org_units(id),
    is_primary BOOLEAN NOT NULL DEFAULT true,
    valid_from TIMESTAMPTZ NOT NULL,
    valid_to TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_user_org_assignments_tenant_user
  ON user_org_assignments(tenant_id, user_id, valid_from);
