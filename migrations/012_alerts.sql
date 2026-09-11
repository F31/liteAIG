-- owner: observability
CREATE TABLE alert_rules (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    name TEXT NOT NULL,
    rule_type TEXT NOT NULL CHECK (rule_type IN ('budget','rate','cost_anomaly')),
    metric TEXT NOT NULL,
    operator TEXT NOT NULL CHECK (operator IN ('gt','gte','lt','lte')),
    threshold DOUBLE PRECISION NOT NULL,
    window_seconds BIGINT NOT NULL,
    severity TEXT NOT NULL CHECK (severity IN ('low','medium','high','critical')),
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(tenant_id, name)
);

CREATE TABLE alerts (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    rule_id UUID NOT NULL REFERENCES alert_rules(id),
    severity TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('firing','acknowledged','silenced','resolved')),
    message TEXT NOT NULL,
    evidence TEXT NOT NULL DEFAULT '{}',
    window_start TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    fired_at TIMESTAMPTZ NOT NULL,
    acked_at TIMESTAMPTZ NULL,
    resolved_at TIMESTAMPTZ NULL,
    silenced_until TIMESTAMPTZ NULL
);
CREATE INDEX idx_alerts_tenant_status ON alerts(tenant_id, status, fired_at);
