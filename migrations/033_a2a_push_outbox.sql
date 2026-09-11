-- owner: federation
-- Durable A2A push callback outbox. Rows are tenant-scoped by joining the
-- completed durable A2A task at enqueue time; callback URLs/tokens are stored
-- for bounded retry and never logged.
CREATE TABLE a2a_push_outbox (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    callback_url TEXT NOT NULL,
    bearer_token TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
      CHECK (status IN ('pending','sending','delivered','failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (id),
    FOREIGN KEY (tenant_id, task_id) REFERENCES a2a_tasks(tenant_id, task_id)
);

CREATE INDEX idx_a2a_push_outbox_due ON a2a_push_outbox(status, next_attempt_at);
CREATE INDEX idx_a2a_push_outbox_task ON a2a_push_outbox(tenant_id, task_id);
