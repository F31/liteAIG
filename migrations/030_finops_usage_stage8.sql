-- owner: finops
ALTER TABLE usage_events ADD COLUMN cache_read_tokens BIGINT NOT NULL DEFAULT 0;
ALTER TABLE usage_events ADD COLUMN cache_write_tokens BIGINT NOT NULL DEFAULT 0;
ALTER TABLE usage_events ADD COLUMN cached_input_tokens BIGINT NOT NULL DEFAULT 0;
ALTER TABLE usage_events ADD COLUMN reasoning_tokens BIGINT NOT NULL DEFAULT 0;
ALTER TABLE usage_events ADD COLUMN tool_calls BIGINT NOT NULL DEFAULT 0;
ALTER TABLE usage_events ADD COLUMN estimation_method TEXT NULL;
