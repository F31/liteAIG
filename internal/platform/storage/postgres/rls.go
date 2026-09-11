package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

// RLS roles: the restricted application role is subject to RLS; the controlled
// platform role bypasses RLS and is used only for operations.
const (
	RoleApp      = "liteaig_app"
	RolePlatform = "liteaig_platform"
)

// rlsTable describes a tenant-scoped table and its RLS USING expression.
type rlsTable struct {
	name  string
	using string // NULL app.tenant_id grants visibility to shared rows
}

// tenantScopedTables lists tables where RLS must be a second defense layer.
var tenantScopedTables = []rlsTable{
	{name: "tenants", using: `id = current_setting('app.tenant_id', true)::uuid`},
	{name: "projects", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	{name: "api_keys", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	{name: "model_deployments", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	{name: "logical_models", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	{name: "route_policies", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	{name: "request_records", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	{name: "usage_events", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	{name: "budget_state", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	{name: "budget_reservations", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	{name: "org_units", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	{name: "user_org_assignments", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	{name: "cost_centers", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	{name: "security_events", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	{name: "tenant_memberships", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	{name: "alert_rules", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	{name: "alerts", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	{name: "notification_settings", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	{name: "guardrail_policies", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	{name: "tool_call_events", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	{name: "approval_requests", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	{name: "federation_relationships", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	// a2a_tasks stores tenant_id as TEXT (unlike the UUID-typed relationship
	// tables), so these RLS predicates compare text to text without a cast.
	{name: "delegation_grants", using: `tenant_id = current_setting('app.tenant_id', true)`},
	{name: "a2a_tasks", using: `tenant_id = current_setting('app.tenant_id', true)`},
	{name: "a2a_push_outbox", using: `tenant_id = current_setting('app.tenant_id', true)`},
	// Tables with nullable tenant_id may hold SYSTEM_SHARED/system-scope rows.
	{name: "providers", using: `(tenant_id = current_setting('app.tenant_id', true)::uuid OR tenant_id IS NULL)`},
	{name: "provider_credentials", using: `(tenant_id = current_setting('app.tenant_id', true)::uuid OR tenant_id IS NULL)`},
	{name: "config_drafts", using: `(tenant_id = current_setting('app.tenant_id', true)::uuid OR tenant_id IS NULL)`},
	{name: "config_versions", using: `(tenant_id = current_setting('app.tenant_id', true)::uuid OR tenant_id IS NULL)`},
	{name: "audit_events", using: `(tenant_id = current_setting('app.tenant_id', true)::uuid OR tenant_id IS NULL)`},
	{name: "audit_retention", using: `tenant_id = current_setting('app.tenant_id', true)::uuid`},
	// Shared key-pepper material lives in NULL-tenant rows, visible to all tenants.
	{name: "secret_material", using: `(tenant_id = current_setting('app.tenant_id', true)::uuid OR tenant_id IS NULL)`},
}

// EnableRLS creates the restricted app role, the controlled platform role, and
// per-table policies so RLS acts as a second defense layer. Idempotent for tests.
func EnableRLS(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE ROLE `+RoleApp+` NOLOGIN`); err != nil {
		// role already exists
	}
	if _, err := db.ExecContext(ctx, `CREATE ROLE `+RolePlatform+` NOLOGIN BYPASSRLS`); err != nil {
		// role already exists
	}
	// Postgres 15+ revokes public schema USAGE by default; grant it to both roles.
	if _, err := db.ExecContext(ctx, `GRANT USAGE ON SCHEMA public TO `+RoleApp+`, `+RolePlatform); err != nil {
		return err
	}
	for _, table := range tenantScopedTables {
		if err := enableTableRLS(ctx, db, table); err != nil {
			return err
		}
	}
	return nil
}

func enableTableRLS(ctx context.Context, db *sql.DB, table rlsTable) error {
	statements := []string{
		fmt.Sprintf(`ALTER TABLE %s ENABLE ROW LEVEL SECURITY`, table.name),
		fmt.Sprintf(`ALTER TABLE %s FORCE ROW LEVEL SECURITY`, table.name),
		fmt.Sprintf(`DROP POLICY IF EXISTS tenant_isolation ON %s`, table.name),
		fmt.Sprintf(`CREATE POLICY tenant_isolation ON %s USING (%s)`, table.name, table.using),
		fmt.Sprintf(`GRANT SELECT, INSERT, UPDATE, DELETE ON %s TO %s`, table.name, RoleApp),
		fmt.Sprintf(`GRANT SELECT, INSERT, UPDATE, DELETE ON %s TO %s`, table.name, RolePlatform),
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("enable RLS on %s: %w", table.name, err)
		}
	}
	return nil
}
