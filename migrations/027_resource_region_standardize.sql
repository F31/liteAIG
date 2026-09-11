-- owner: catalog
-- Standardize the data-region catalog to a single, consistent granularity:
-- legal jurisdictions (countries / territories / the EU) plus "global".
-- Data residency enforcement matches deployment.data_region against a
-- project's allowed_data_regions by exact string equality, so the catalog
-- must not mix granularities (e.g. country vs cloud region) or a strict
-- project can silently exclude every deployment.

DELETE FROM resource_region_catalog;

INSERT INTO resource_region_catalog(id, label, sort_order) VALUES
    ('global', 'Global', 10),
    ('us', 'United States', 20),
    ('ca', 'Canada', 30),
    ('mx', 'Mexico', 40),
    ('br', 'Brazil', 50),
    ('eu', 'European Union', 60),
    ('uk', 'United Kingdom', 70),
    ('ch', 'Switzerland', 80),
    ('no', 'Norway', 90),
    ('de', 'Germany', 100),
    ('fr', 'France', 110),
    ('es', 'Spain', 120),
    ('it', 'Italy', 130),
    ('nl', 'Netherlands', 140),
    ('se', 'Sweden', 150),
    ('pl', 'Poland', 160),
    ('tr', 'Türkiye', 170),
    ('cn', 'China', 180),
    ('hk', 'Hong Kong (China)', 190),
    ('tw', 'Taiwan', 200),
    ('jp', 'Japan', 210),
    ('kr', 'South Korea', 220),
    ('sg', 'Singapore', 230),
    ('in', 'India', 240),
    ('id', 'Indonesia', 250),
    ('my', 'Malaysia', 260),
    ('th', 'Thailand', 270),
    ('vn', 'Vietnam', 280),
    ('ph', 'Philippines', 290),
    ('au', 'Australia', 300),
    ('nz', 'New Zealand', 310),
    ('za', 'South Africa', 320)
ON CONFLICT(id) DO NOTHING;
