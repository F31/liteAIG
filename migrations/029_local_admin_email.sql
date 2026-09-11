-- owner: identity
ALTER TABLE local_admins ADD COLUMN email TEXT NOT NULL DEFAULT '';
