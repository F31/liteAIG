package sqlrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/F31/liteAIG/internal/guardrail/benchmark"
	"github.com/F31/liteAIG/internal/tenancy"
)

// EvidenceRepository assembles the read-only evidence record stream for the
// Evidence Export capability. Every record is derived from durable, tenant-
// scoped tables; prompt/response bodies and secrets are never selected.
type EvidenceRepository struct{ db *sql.DB }

func NewEvidenceRepository(db *sql.DB) *EvidenceRepository { return &EvidenceRepository{db: db} }

// Collect returns evidence records covering config/policy versions, RBAC role
// assignments, audit events, guardrail events, provider processing
// destinations, secret rotation, and federation history within [from, to).
// A zero from or to bound is treated as unbounded on that side. Provider
// destinations have no timestamp and are always returned as current inventory.
func (r *EvidenceRepository) Collect(ctx context.Context, scope tenancy.TenantScope, from, to time.Time) ([]benchmark.EvidenceRecord, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	var records []benchmark.EvidenceRecord
	collectors := []func(context.Context, tenancy.TenantScope, time.Time, time.Time) ([]benchmark.EvidenceRecord, error){
		r.configVersions,
		r.guardrailPolicies,
		r.rbacAssignments,
		r.auditEvents,
		r.securityEvents,
		r.providerDestinations,
		r.secretRotation,
		r.federationHistory,
	}
	for _, collect := range collectors {
		got, err := collect(ctx, scope, from, to)
		if err != nil {
			return nil, err
		}
		records = append(records, got...)
	}
	return records, nil
}

func record(kind, tenantID, resourceID string, version, epoch int64, at time.Time, detail map[string]any) benchmark.EvidenceRecord {
	return benchmark.EvidenceRecord{
		Kind:          kind,
		TenantID:      tenantID,
		ResourceID:    resourceID,
		Version:       version,
		SecurityEpoch: epoch,
		OccurredAt:    at,
		Detail:        stringDetail(detail),
	}
}

func stringDetail(detail map[string]any) map[string]string {
	out := make(map[string]string, len(detail))
	for key, value := range detail {
		switch v := value.(type) {
		case string:
			out[key] = v
		case int64:
			out[key] = fmt.Sprintf("%d", v)
		case float64:
			out[key] = fmt.Sprintf("%g", v)
		case bool:
			out[key] = fmt.Sprintf("%t", v)
		default:
			encoded, err := json.Marshal(value)
			if err == nil {
				out[key] = string(encoded)
			}
		}
	}
	return out
}

// timeRange composes the SQL predicate and arguments for one timestamp
// column. Zero bounds select the whole history, so the export stays fully
// deterministic on the caller's own filter. All callers reserve $1 for tenant ID.
func timeRange(column string, from, to time.Time) (string, []any) {
	var clauses []string
	var args []any
	if !from.IsZero() {
		args = append(args, from.UTC())
		clauses = append(clauses, fmt.Sprintf("%s >= $%d", column, len(args)+1))
	}
	if !to.IsZero() {
		args = append(args, to.UTC())
		clauses = append(clauses, fmt.Sprintf("%s < $%d", column, len(args)+1))
	}
	if len(clauses) == 0 {
		return "1=1", nil
	}
	predicate := clauses[0]
	for _, clause := range clauses[1:] {
		predicate += " AND " + clause
	}
	return predicate, args
}

func (r *EvidenceRepository) configVersions(ctx context.Context, scope tenancy.TenantScope, from, to time.Time) ([]benchmark.EvidenceRecord, error) {
	predicate, args := timeRange("published_at", from, to)
	rows, err := r.db.QueryContext(ctx, `
SELECT id, tenant_id, version, source_draft_id, COALESCE(published_by,''), published_at
FROM config_versions WHERE tenant_id = $1 AND `+predicate+`
ORDER BY version`, append([]any{scope.TenantID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []benchmark.EvidenceRecord
	for rows.Next() {
		var id, tenantID, publishedBy string
		var version int64
		var sourceDraftID sql.NullString
		var publishedAt databaseTime
		if err := rows.Scan(&id, &tenantID, &version, &sourceDraftID, &publishedBy, &publishedAt); err != nil {
			return nil, err
		}
		records = append(records, record("config.version", tenantID, id, version, 0, publishedAt.Time, map[string]any{
			"source_draft_id": sourceDraftID.String,
			"published_by":    publishedBy,
		}))
	}
	return records, rows.Err()
}

func (r *EvidenceRepository) guardrailPolicies(ctx context.Context, scope tenancy.TenantScope, from, to time.Time) ([]benchmark.EvidenceRecord, error) {
	predicate, args := timeRange("published_at", from, to)
	rows, err := r.db.QueryContext(ctx, `
SELECT policy_id, tenant_id, version, security_epoch, change_type, actor, published_at
FROM guardrail_policies WHERE tenant_id = $1 AND `+predicate+`
ORDER BY version`, append([]any{scope.TenantID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []benchmark.EvidenceRecord
	for rows.Next() {
		var policyID, tenantID, changeType, actor string
		var version, epoch int64
		var publishedAt databaseTime
		if err := rows.Scan(&policyID, &tenantID, &version, &epoch, &changeType, &actor, &publishedAt); err != nil {
			return nil, err
		}
		records = append(records, record("guardrail.policy", tenantID, policyID, version, epoch, publishedAt.Time, map[string]any{
			"change_type": changeType,
			"actor":       actor,
		}))
	}
	return records, rows.Err()
}

func (r *EvidenceRepository) rbacAssignments(ctx context.Context, scope tenancy.TenantScope, from, to time.Time) ([]benchmark.EvidenceRecord, error) {
	predicate, args := timeRange("u.created_at", from, to)
	// Match LocalCredentialStore: local accounts belong to the first active
	// tenant. Organization tenant_memberships do not carry RBAC roles.
	rows, err := r.db.QueryContext(ctx, `
SELECT u.id, u.username, u.role, u.status, COALESCE(u.email,''), u.created_at
FROM local_admins u
WHERE (SELECT id FROM tenants WHERE status = 'active' ORDER BY created_at LIMIT 1) = $1 AND `+predicate+`
ORDER BY u.created_at`, append([]any{scope.TenantID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []benchmark.EvidenceRecord
	for rows.Next() {
		var id, username, role, status, email string
		var createdAt databaseTime
		if err := rows.Scan(&id, &username, &role, &status, &email, &createdAt); err != nil {
			return nil, err
		}
		records = append(records, record("rbac.assignment", scope.TenantID, id, 0, 0, createdAt.Time, map[string]any{
			"username": username,
			"role":     role,
			"status":   status,
			"email":    email,
		}))
	}
	return records, rows.Err()
}

func (r *EvidenceRepository) auditEvents(ctx context.Context, scope tenancy.TenantScope, from, to time.Time) ([]benchmark.EvidenceRecord, error) {
	predicate, args := timeRange("a.occurred_at", from, to)
	rows, err := r.db.QueryContext(ctx, `
SELECT a.id, a.tenant_id, a.scope, a.action, a.resource_type, COALESCE(a.resource_id,''), a.result, COALESCE(lu.username, a.actor_id), a.occurred_at
FROM audit_events a LEFT JOIN local_admins lu ON lu.id = a.actor_id
WHERE a.tenant_id = $1 AND `+predicate+`
ORDER BY a.occurred_at`, append([]any{scope.TenantID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []benchmark.EvidenceRecord
	for rows.Next() {
		var id, tenantID, action, resourceType, resourceID, result, actor string
		var scopeText string
		var occurredAt databaseTime
		if err := rows.Scan(&id, &tenantID, &scopeText, &action, &resourceType, &resourceID, &result, &actor, &occurredAt); err != nil {
			return nil, err
		}
		records = append(records, record("audit", tenantID, id, 0, 0, occurredAt.Time, map[string]any{
			"scope":         scopeText,
			"action":        action,
			"resource_type": resourceType,
			"resource_id":   resourceID,
			"result":        result,
			"actor":         actor,
		}))
	}
	return records, rows.Err()
}

func (r *EvidenceRepository) securityEvents(ctx context.Context, scope tenancy.TenantScope, from, to time.Time) ([]benchmark.EvidenceRecord, error) {
	predicate, args := timeRange("s.occurred_at", from, to)
	rows, err := r.db.QueryContext(ctx, `
SELECT s.id, s.tenant_id, s.policy_id, s.rule_id, s.action, s.content_hash, s.snapshot_version, COALESCE(s.request_id,''), s.occurred_at
FROM security_events s
WHERE s.tenant_id = $1 AND `+predicate+`
ORDER BY s.occurred_at`, append([]any{scope.TenantID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []benchmark.EvidenceRecord
	for rows.Next() {
		var id, tenantID, policyID, ruleID, action, contentHash, requestID string
		var version int64
		var occurredAt databaseTime
		if err := rows.Scan(&id, &tenantID, &policyID, &ruleID, &action, &contentHash, &version, &requestID, &occurredAt); err != nil {
			return nil, err
		}
		records = append(records, record("guardrail.event", tenantID, id, version, 0, occurredAt.Time, map[string]any{
			"policy_id":    policyID,
			"rule_id":      ruleID,
			"action":       action,
			"content_hash": contentHash,
			"request_id":   requestID,
		}))
	}
	return records, rows.Err()
}

func (r *EvidenceRepository) providerDestinations(ctx context.Context, scope tenancy.TenantScope, from, to time.Time) ([]benchmark.EvidenceRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT d.id, d.tenant_id, p.name, p.type, COALESCE(d.upstream_model,''), COALESCE(d.region,''), d.data_region, COALESCE(p.status,'')
FROM model_deployments d JOIN providers p ON d.provider_id = p.id
WHERE d.tenant_id = $1
  AND (p.tenant_id = d.tenant_id OR (p.owner_scope = 'SYSTEM_SHARED' AND p.tenant_id IS NULL))
ORDER BY d.id`, scope.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []benchmark.EvidenceRecord
	for rows.Next() {
		var id, tenantID, name, providerType, upstream, region, dataRegion, status string
		if err := rows.Scan(&id, &tenantID, &name, &providerType, &upstream, &region, &dataRegion, &status); err != nil {
			return nil, err
		}
		records = append(records, record("provider.destination", tenantID, id, 0, 0, time.Time{}, map[string]any{
			"provider":       name,
			"provider_type":  providerType,
			"upstream_model": upstream,
			"region":         region,
			"data_region":    dataRegion,
			"status":         status,
		}))
	}
	return records, rows.Err()
}

func (r *EvidenceRepository) secretRotation(ctx context.Context, scope tenancy.TenantScope, from, to time.Time) ([]benchmark.EvidenceRecord, error) {
	predicate, args := timeRange("COALESCE(rotated_at, created_at)", from, to)
	rows, err := r.db.QueryContext(ctx, `
SELECT secret_ref, tenant_id, status, created_at, rotated_at
FROM secret_material WHERE tenant_id = $1 AND `+predicate+`
ORDER BY COALESCE(rotated_at, created_at)`, append([]any{scope.TenantID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []benchmark.EvidenceRecord
	for rows.Next() {
		var ref, tenantID, status string
		var createdAt databaseTime
		var rotatedAt nullableDatabaseTime
		if err := rows.Scan(&ref, &tenantID, &status, &createdAt, &rotatedAt); err != nil {
			return nil, err
		}
		at := createdAt.Time
		rotated := ""
		if rotatedAt.Valid {
			at = rotatedAt.Time
			rotated = rotatedAt.Time.UTC().Format(time.RFC3339)
		}
		records = append(records, record("secret.rotation", tenantID, ref, 0, 0, at, map[string]any{
			"status":     status,
			"created_at": createdAt.Time.UTC().Format(time.RFC3339),
			"rotated_at": rotated,
		}))
	}
	return records, rows.Err()
}

func (r *EvidenceRepository) federationHistory(ctx context.Context, scope tenancy.TenantScope, from, to time.Time) ([]benchmark.EvidenceRecord, error) {
	predicate, args := timeRange("updated_at", from, to)
	rows, err := r.db.QueryContext(ctx, `
SELECT id, tenant_id, external_agent_id, name, status, created_at, updated_at
FROM federation_relationships WHERE tenant_id = $1 AND `+predicate+`
ORDER BY updated_at`, append([]any{scope.TenantID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []benchmark.EvidenceRecord
	for rows.Next() {
		var id, tenantID, externalID, name, status string
		var createdAt, updatedAt databaseTime
		if err := rows.Scan(&id, &tenantID, &externalID, &name, &status, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		records = append(records, record("federation.relationship", tenantID, id, 0, 0, updatedAt.Time, map[string]any{
			"external_agent_id": externalID,
			"name":              name,
			"status":            status,
			"created_at":        createdAt.Time.UTC().Format(time.RFC3339),
		}))
	}
	return records, rows.Err()
}
