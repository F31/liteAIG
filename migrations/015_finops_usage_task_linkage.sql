-- owner: finops
ALTER TABLE usage_events ADD COLUMN session_id TEXT NULL;
ALTER TABLE usage_events ADD COLUMN task_id TEXT NULL;
ALTER TABLE usage_events ADD COLUMN root_task_id TEXT NULL;
ALTER TABLE usage_events ADD COLUMN parent_task_id TEXT NULL;
ALTER TABLE usage_events ADD COLUMN agent_id TEXT NULL;
CREATE INDEX idx_usage_events_tenant_task ON usage_events(tenant_id, root_task_id, task_id);
