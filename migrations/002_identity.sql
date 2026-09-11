-- owner: identity
CREATE TABLE local_admins (
    id UUID PRIMARY KEY,
    singleton_key INTEGER NOT NULL DEFAULT 1 UNIQUE CHECK (singleton_key = 1),
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active'
      CHECK (status IN ('active','disabled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE key_pepper_versions (
    version INTEGER PRIMARY KEY,
    pepper_ref TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active','retiring','retired')),
    activated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE api_keys (
    id UUID PRIMARY KEY,
    public_id TEXT NOT NULL,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    project_id UUID NOT NULL REFERENCES projects(id),
    name TEXT NOT NULL,
    hmac_digest BYTEA NOT NULL,
    pepper_version INTEGER NOT NULL,
    fingerprint TEXT NOT NULL,
    application_id UUID NULL,
    agent_id UUID NULL,
    service_account_id UUID NULL,
    status TEXT NOT NULL CHECK (status IN ('active','disabled','revoked')),
    expires_at TIMESTAMPTZ NULL,
    model_allowlist TEXT NULL,
    ip_allowlist TEXT NULL,
    budget_policy_id UUID NULL,
    rate_policy_id UUID NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    revoked_at TIMESTAMPTZ NULL,
    UNIQUE(tenant_id, public_id)
);

CREATE INDEX idx_api_keys_tenant_project ON api_keys(tenant_id, project_id);
