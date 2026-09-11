-- owner: finops
ALTER TABLE usage_events ADD COLUMN user_id UUID NULL;
ALTER TABLE usage_events ADD COLUMN org_unit_id UUID NULL;
ALTER TABLE usage_events ADD COLUMN org_path_snapshot TEXT NULL;
ALTER TABLE usage_events ADD COLUMN cost_center_id UUID NULL;
ALTER TABLE usage_events ADD COLUMN attribution_trust TEXT NOT NULL DEFAULT 'none';
