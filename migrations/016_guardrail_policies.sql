-- owner: guardrail
CREATE TABLE guardrail_policies (
    policy_id TEXT NOT NULL,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    version BIGINT NOT NULL,
    security_epoch BIGINT NOT NULL,
    change_type TEXT NOT NULL CHECK (change_type IN ('tighten','loosen')),
    rules TEXT NOT NULL DEFAULT '[]',
    actor TEXT NOT NULL,
    published_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, version)
);
CREATE INDEX idx_guardrail_policies_tenant ON guardrail_policies(tenant_id, version DESC);
