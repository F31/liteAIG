-- owner: observability
ALTER TABLE notification_settings ADD COLUMN targets TEXT NOT NULL DEFAULT '[]';
ALTER TABLE notification_settings ADD COLUMN dedup_seconds INTEGER NOT NULL DEFAULT 300;