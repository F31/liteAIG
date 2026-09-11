-- owner: identity
-- Allow operator-seeded system admins while keeping tenant-local user flows
-- constrained in the application layer.
CREATE TABLE local_admins_new (
    id UUID PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'tenant_admin'
      CHECK (role IN ('system_admin','tenant_admin','tenant_operator','viewer')),
    status TEXT NOT NULL DEFAULT 'active'
      CHECK (status IN ('active','disabled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    email TEXT NOT NULL DEFAULT ''
);

INSERT INTO local_admins_new(id, username, password_hash, role, status, created_at, email)
  SELECT id, username, password_hash, COALESCE(role, 'tenant_admin'), status, created_at, COALESCE(email, '')
  FROM local_admins;

DROP TABLE local_admins;

ALTER TABLE local_admins_new RENAME TO local_admins;
