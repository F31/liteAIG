-- owner: finops
ALTER TABLE usage_events ADD COLUMN agent_version TEXT NULL;
ALTER TABLE usage_events ADD COLUMN agent_endpoint_id TEXT NULL;
