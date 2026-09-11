-- owner: identity
-- Persistent key pepper ciphertext so virtual API keys survive restarts,
-- and a generic envelope-encrypted secret vault keyed by opaque ref
-- (local://credential/<id> provider credentials, secret://env/... overrides).
CREATE TABLE key_pepper_secrets (
    version INTEGER PRIMARY KEY,
    ciphertext BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE secret_material (
    secret_ref TEXT PRIMARY KEY,
    tenant_id UUID NULL,
    ciphertext BYTEA NOT NULL,
    status TEXT NOT NULL DEFAULT 'enabled'
      CHECK (status IN ('enabled','disabled','rotated')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    rotated_at TIMESTAMPTZ NULL
);

CREATE INDEX idx_secret_material_tenant ON secret_material(tenant_id);
