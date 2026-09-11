-- owner: observability
ALTER TABLE request_records ADD COLUMN user_id UUID NULL;
ALTER TABLE request_records ADD COLUMN org_unit_id UUID NULL;
ALTER TABLE request_records ADD COLUMN org_path_snapshot TEXT NULL;
ALTER TABLE request_records ADD COLUMN cost_center_id UUID NULL;
ALTER TABLE request_records ADD COLUMN attribution_trust TEXT NOT NULL DEFAULT 'none';
