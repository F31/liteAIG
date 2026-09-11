-- owner: observability
ALTER TABLE request_records ADD COLUMN session_id TEXT NULL;
ALTER TABLE request_records ADD COLUMN task_id TEXT NULL;
ALTER TABLE request_records ADD COLUMN root_task_id TEXT NULL;
ALTER TABLE request_records ADD COLUMN parent_task_id TEXT NULL;
ALTER TABLE request_records ADD COLUMN agent_id TEXT NULL;
CREATE INDEX idx_request_records_tenant_task ON request_records(tenant_id, root_task_id, task_id);
