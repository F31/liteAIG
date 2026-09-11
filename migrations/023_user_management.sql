-- owner: identity
-- Multi-user local accounts: drop the single-admin singleton constraint and
-- add the tenant role. SQLite cannot drop a column, so the table is rebuilt.
CREATE TABLE local_admins_new (
    id UUID PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'tenant_admin'
      CHECK (role IN ('tenant_admin','tenant_operator','viewer')),
    status TEXT NOT NULL DEFAULT 'active'
      CHECK (status IN ('active','disabled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO local_admins_new(id, username, password_hash, role, status, created_at)
  SELECT id, username, password_hash, 'tenant_admin', status, created_at
  FROM local_admins;

DROP TABLE local_admins;

ALTER TABLE local_admins_new RENAME TO local_admins;
