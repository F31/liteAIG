-- owner: observability
ALTER TABLE request_records ADD COLUMN agent_version TEXT NULL;
ALTER TABLE request_records ADD COLUMN agent_endpoint_id TEXT NULL;
