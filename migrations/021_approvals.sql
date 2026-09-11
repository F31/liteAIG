-- owner: controlplane
CREATE TABLE approval_requests (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    requester TEXT NOT NULL,
    action TEXT NOT NULL,
    target TEXT NOT NULL,
    dual_approval BOOLEAN NOT NULL DEFAULT FALSE,
    status TEXT NOT NULL DEFAULT 'pending'
      CHECK (status IN ('pending','approved','rejected','cancelled')),
    approved_by TEXT NOT NULL DEFAULT '[]',
    rejected_by TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_approval_requests_tenant ON approval_requests(tenant_id, status);
