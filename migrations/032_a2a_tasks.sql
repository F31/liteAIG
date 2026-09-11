-- owner: federation
-- Durable A2A task state for the outbound relay: one row per durable task
-- (task_id = the Lite request id that first created it), deduplicated by
-- idempotency key, with persisted hop/call/attempt counters. message is a
-- small snapshot of the outbound text we send (never secrets) and result is
-- the completion text reused by idempotent replays.
CREATE TABLE a2a_tasks (
    task_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    external_agent_id TEXT NOT NULL DEFAULT '',
    project_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending'
      CHECK (status IN ('pending','running','completed','failed','cancelled')),
    request_id TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    result TEXT NOT NULL DEFAULT '',
    hop_count INTEGER NOT NULL DEFAULT 0,
    call_count INTEGER NOT NULL DEFAULT 0,
    attempts INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (tenant_id, task_id),
    UNIQUE (tenant_id, idempotency_key)
);

CREATE INDEX idx_a2a_tasks_tenant_status ON a2a_tasks(tenant_id, status);
