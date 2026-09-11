-- owner: finops
CREATE TABLE budget_reservations (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    reservation_id TEXT NOT NULL,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('tenant','project','key')),
    scope_id TEXT NULL,
    estimate NUMERIC(18,9) NOT NULL,
    actual NUMERIC(18,9) NULL,
    status TEXT NOT NULL CHECK (status IN ('reserved','reconciled','released','expired')),
    window_keys TEXT NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finalized_at TIMESTAMPTZ NULL,
    UNIQUE(tenant_id, reservation_id)
);
CREATE INDEX idx_budget_reservations_tenant_created
  ON budget_reservations(tenant_id, created_at);
