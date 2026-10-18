-- owner: observability
-- Mirror the auxiliary-call parent link on request_records so the UI can trace
-- an embedding/judge request back to the human/API request that caused it.
ALTER TABLE request_records ADD COLUMN parent_request_id TEXT NULL;

CREATE INDEX idx_request_records_tenant_parent ON request_records(tenant_id, parent_request_id);
