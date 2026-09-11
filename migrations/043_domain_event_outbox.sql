-- owner: observability
CREATE TABLE domain_event_outbox (
    id UUID PRIMARY KEY,
    kind TEXT NOT NULL,
    tenant_id TEXT NULL,
    project_id TEXT NULL,
    request_id TEXT NULL,
    attributes TEXT NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','claiming','sent','failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_domain_event_outbox_due ON domain_event_outbox(status, next_attempt_at);
CREATE INDEX idx_domain_event_outbox_tenant ON domain_event_outbox(tenant_id, occurred_at);
