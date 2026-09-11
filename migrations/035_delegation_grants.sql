-- owner: identity
CREATE TABLE delegation_grants (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    delegator_id TEXT NOT NULL,
    delegatee_id TEXT NOT NULL,
    permissions_json TEXT NOT NULL DEFAULT '[]',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, delegator_id, delegatee_id)
);

CREATE INDEX idx_delegation_grants_tenant_delegator ON delegation_grants(tenant_id, delegator_id);
