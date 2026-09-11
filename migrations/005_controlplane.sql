-- owner: controlplane
CREATE TABLE config_drafts (
    id UUID PRIMARY KEY,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('system','tenant')),
    tenant_id UUID NULL REFERENCES tenants(id),
    base_version BIGINT NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    status TEXT NOT NULL
      CHECK (status IN ('editing','pending_approval','published','discarded','expired')),
    changes TEXT NOT NULL DEFAULT '[]',
    source_type TEXT NOT NULL DEFAULT 'manual'
      CHECK (source_type IN ('manual','system_recommendation')),
    source_ref UUID NULL,
    created_by UUID NOT NULL,
    updated_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NULL
);

CREATE INDEX idx_config_drafts_tenant_status
  ON config_drafts(tenant_id, status, updated_at);

CREATE TABLE config_versions (
    id UUID PRIMARY KEY,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('system','tenant')),
    tenant_id UUID NULL REFERENCES tenants(id),
    version BIGINT NOT NULL,
    source_draft_id UUID NULL REFERENCES config_drafts(id),
    compiled_config TEXT NOT NULL,
    published_by UUID NOT NULL,
    published_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(scope_type, tenant_id, version)
);
