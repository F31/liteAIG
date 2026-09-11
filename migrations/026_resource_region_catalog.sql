-- owner: catalog
CREATE TABLE resource_region_catalog (
    id TEXT PRIMARY KEY,
    label TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    sort_order INTEGER NOT NULL DEFAULT 0
);

INSERT INTO resource_region_catalog(id, label, sort_order) VALUES
('global', 'Global', 10),
('us', 'United States', 20),
('us-east-1', 'US East', 30),
('us-west-2', 'US West', 40),
('eu', 'Europe', 50),
('eu-west-1', 'EU West', 60),
('eu-central-1', 'EU Central', 70),
('uk', 'United Kingdom', 80),
('apac', 'Asia Pacific', 90),
('ap-southeast-1', 'Singapore', 100),
('ap-northeast-1', 'Japan', 110),
('cn', 'China', 120),
('cn-north', 'China North', 130),
('cn-east', 'China East', 140),
('hk', 'Hong Kong', 150),
('au', 'Australia', 160)
ON CONFLICT(id) DO NOTHING;
