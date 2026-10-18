-- owner: finops
-- Link auxiliary provider calls (semantic-cache embeddings, LLM judge) to the
-- main request that triggered them without changing the one-usage-per-request
-- uniqueness constraint. Auxiliary calls get their own request/usage IDs.
ALTER TABLE usage_events ADD COLUMN parent_request_id TEXT NULL;

CREATE INDEX idx_usage_events_tenant_parent ON usage_events(tenant_id, parent_request_id);
