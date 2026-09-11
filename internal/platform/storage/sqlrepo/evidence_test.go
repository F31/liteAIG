package sqlrepo

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/platform/storage/sqlite"
	"github.com/F31/liteAIG/internal/tenancy"
	"github.com/F31/liteAIG/migrations"
)

func evidenceTestStore(t *testing.T, name string) *Store {
	t.Helper()
	db, err := sqlite.Open(context.Background(), "file:evidence-"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return Open(db, Deps{})
}

func evidenceExec(t *testing.T, store *Store, query string, args ...any) {
	t.Helper()
	if _, err := store.db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("seed evidence: %v\n%s", err, query)
	}
}

// Local roles are global identities scoped by authentication to the oldest
// active tenant, not organization membership roles.
func seedEvidence(t *testing.T, store *Store, tenant, id string, version int, at time.Time, localRole bool) {
	t.Helper()
	evidenceExec(t, store, `INSERT INTO config_versions
(id, scope_type, tenant_id, version, compiled_config, published_by, published_at)
VALUES ($1, 'tenant', $2, $3, '{"prompt":"sensitive-config"}', 'publisher', $4)`, id, tenant, version, at)
	evidenceExec(t, store, `INSERT INTO guardrail_policies
(policy_id, tenant_id, version, security_epoch, change_type, rules, actor, published_at)
VALUES ($1, $2, $3, 9, 'tighten', '["sensitive-rules"]', 'operator', $4)`, id, tenant, version, at)
	if localRole {
		evidenceExec(t, store, `INSERT INTO local_admins
(id, username, password_hash, role, status, created_at)
VALUES ($1, $1, 'sensitive-password', 'tenant_operator', 'active', $2)`, id, at)
	}
	evidenceExec(t, store, `INSERT INTO audit_events
(id, tenant_id, scope, actor_id, action, resource_type, result, details, occurred_at)
VALUES ($1, $2, 'tenant', 'actor', 'publish', 'config', 'success', '{"response":"sensitive-audit"}', $3)`, id, tenant, at)
	evidenceExec(t, store, `INSERT INTO security_events
(id, tenant_id, policy_id, rule_id, action, content_hash, snapshot_version, occurred_at)
VALUES ($1, $2, 'policy', 'rule', 'block', 'content-digest', 7, $3)`, id, tenant, at)
	evidenceExec(t, store, `INSERT INTO providers
(id, tenant_id, owner_scope, name, type, config)
VALUES ($1, $2, 'TENANT_PRIVATE', 'provider', 'openai', '{"token":"sensitive-provider"}')`, id, tenant)
	evidenceExec(t, store, `INSERT INTO provider_credentials
(id, provider_id, tenant_id, owner_scope, label, secret_ref, fingerprint)
VALUES ($1, $1, $2, 'TENANT_PRIVATE', 'credential', 'sensitive-credential', 'fingerprint')`, id, tenant)
	evidenceExec(t, store, `INSERT INTO model_deployments
(id, tenant_id, provider_id, credential_id, upstream_model, endpoint, region, data_region, capabilities)
VALUES ($1, $2, $1, $1, 'model', 'https://sensitive-endpoint', 'us', 'us', '{"body":"sensitive-capabilities"}')`, id, tenant)
	// Exercise both NULL and non-NULL SQLite TIMESTAMPTZ values. For rotated
	// secrets the creation timestamp must not control the export window.
	createdAt := at
	var rotatedAt any
	if version%2 == 0 {
		createdAt = at.Add(-72 * time.Hour)
		rotatedAt = at
	}
	evidenceExec(t, store, `INSERT INTO secret_material
(secret_ref, tenant_id, ciphertext, status, created_at, rotated_at)
VALUES ($1, $2, $3, 'enabled', $4, $5)`, id, tenant, []byte("sensitive-ciphertext"), createdAt, rotatedAt)
	evidenceExec(t, store, `INSERT INTO federation_relationships
(id, tenant_id, external_agent_id, name, status, body, created_at, updated_at)
VALUES ($1, $2, 'external', 'partner', 'trusted', '{"body":"sensitive-federation"}', $3, $4)`, id, tenant, at.Add(-72*time.Hour), at)
}

func TestEvidenceCollect(t *testing.T) {
	store := evidenceTestStore(t, "collect")
	ctx := context.Background()
	base := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	tenants := []string{"00000000-0000-0000-0000-00000000000a", "00000000-0000-0000-0000-00000000000b"}
	kinds := []string{"config.version", "guardrail.policy", "rbac.assignment", "audit", "guardrail.event", "provider.destination", "secret.rotation", "federation.relationship"}
	for n, tenant := range tenants {
		evidenceExec(t, store, `INSERT INTO tenants(id, public_ref, name, created_at)
VALUES ($1, $1, $1, $2)`, tenant, base.Add(time.Duration(n-100)*time.Hour))
		for i := 0; i < 4; i++ {
			seedEvidence(t, store, tenant, fmt.Sprintf("%s-%d", tenant, i), i+1, base.Add(time.Duration(i)*time.Hour), n == 0)
		}
		// Organization membership is not itself an RBAC role assignment.
		evidenceExec(t, store, `INSERT INTO users(id, display_name) VALUES ($1, 'member')`, tenant)
		evidenceExec(t, store, `INSERT INTO tenant_memberships(tenant_id, user_id, joined_at) VALUES ($1, $1, $2)`, tenant, base)
	}
	tests := []struct {
		name     string
		from, to time.Time
		indices  []int
	}{
		{name: "unbounded", indices: []int{0, 1, 2, 3}},
		{name: "lower-inclusive", from: base.Add(time.Hour), indices: []int{1, 2, 3}},
		{name: "upper-exclusive", to: base.Add(2 * time.Hour), indices: []int{0, 1}},
		{name: "both", from: base.Add(time.Hour), to: base.Add(3 * time.Hour), indices: []int{1, 2}},
		{name: "equal", from: base, to: base},
		{name: "reversed", from: base.Add(time.Hour), to: base},
		{name: "empty", from: base.Add(4 * time.Hour)},
		{name: "offset", from: base.Add(time.Hour).In(time.FixedZone("offset", 2*60*60)), to: base.Add(3 * time.Hour), indices: []int{1, 2}},
	}
	for n, tenant := range tenants {
		for _, tc := range tests {
			t.Run(fmt.Sprintf("tenant-%d/%s", n, tc.name), func(t *testing.T) {
				records, err := store.Evidence.Collect(ctx, tenancy.TenantScope{TenantID: tenant}, tc.from, tc.to)
				if err != nil {
					t.Fatal(err)
				}
				want := map[string]map[string]bool{}
				for _, kind := range kinds {
					indices := tc.indices
					if kind == "rbac.assignment" && n != 0 {
						continue
					}
					if kind == "provider.destination" {
						indices = []int{0, 1, 2, 3} // Current inventory has no timestamp.
					}
					for _, i := range indices {
						if want[kind] == nil {
							want[kind] = map[string]bool{}
						}
						want[kind][fmt.Sprintf("%s-%d", tenant, i)] = true
					}
				}
				got := map[string]map[string]bool{}
				for _, record := range records {
					if record.TenantID != tenant {
						t.Fatalf("cross-tenant evidence: %+v", record)
					}
					if got[record.Kind] == nil {
						got[record.Kind] = map[string]bool{}
					}
					if got[record.Kind][record.ResourceID] {
						t.Fatalf("duplicate evidence: %+v", record)
					}
					got[record.Kind][record.ResourceID] = true
					if record.Kind == "provider.destination" {
						if !record.OccurredAt.IsZero() {
							t.Fatalf("destination should be untimed: %+v", record)
						}
					} else {
						var index int
						if _, err := fmt.Sscanf(strings.TrimPrefix(record.ResourceID, tenant+"-"), "%d", &index); err != nil {
							t.Fatal(err)
						}
						if !record.OccurredAt.Equal(base.Add(time.Duration(index) * time.Hour)) {
							t.Fatalf("incorrect occurrence time: %+v", record)
						}
						if record.Kind == "secret.rotation" {
							wantRotation := ""
							if index%2 == 1 {
								wantRotation = record.OccurredAt.Format(time.RFC3339)
							}
							if record.Detail["rotated_at"] != wantRotation {
								t.Fatalf("incorrect rotation detail: %+v", record)
							}
						}
					}
					if record.Kind == "rbac.assignment" && record.Detail["role"] != "tenant_operator" {
						t.Fatalf("incorrect role: %+v", record)
					}
					if record.Kind == "guardrail.policy" && (record.SecurityEpoch != 9 || record.Detail["change_type"] != "tighten") {
						t.Fatalf("incorrect policy metadata: %+v", record)
					}
					if record.Kind == "guardrail.event" && (record.Version != 7 || record.Detail["content_hash"] != "content-digest") {
						t.Fatalf("incorrect event metadata: %+v", record)
					}
					for _, key := range []string{"compiled_config", "rules", "password_hash", "ciphertext", "body", "details", "config", "endpoint", "capabilities", "prompt", "response"} {
						if _, ok := record.Detail[key]; ok {
							t.Fatalf("sensitive field %s in %+v", key, record)
						}
					}
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("evidence IDs by kind:\ngot  %v\nwant %v", got, want)
				}
				encoded, err := json.Marshal(records)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(encoded), "sensitive-") {
					t.Fatalf("sensitive body leaked: %s", encoded)
				}
			})
		}
	}
	if _, err := store.Evidence.Collect(ctx, tenancy.TenantScope{}, time.Time{}, time.Time{}); err == nil {
		t.Fatal("expected invalid scope error")
	}
}

func TestEvidenceRBACMatchesIdentityScope(t *testing.T) {
	store := evidenceTestStore(t, "identity")
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, id := range []string{"suspended", "first", "second"} {
		evidenceExec(t, store, `INSERT INTO tenants(id, public_ref, name, created_at) VALUES ($1, $1, $1, $2)`, id, base.Add(time.Duration(i)*time.Hour))
	}
	evidenceExec(t, store, `UPDATE tenants SET status = 'suspended' WHERE id = 'suspended'`)
	evidenceExec(t, store, `INSERT INTO local_admins(id, username, password_hash, role, created_at)
VALUES ('admin', 'ops', 'sensitive-password', 'viewer', $1)`, base)
	for _, owner := range []string{"first", "second"} {
		credential, err := NewLocalCredentialStore(store.db).FindLocalCredential(ctx, "ops")
		if err != nil || credential.TenantID != owner {
			t.Fatalf("identity scope: %+v, %v", credential, err)
		}
		for _, tenant := range []string{"suspended", "first", "second", "absent"} {
			records, err := store.Evidence.Collect(ctx, tenancy.TenantScope{TenantID: tenant}, time.Time{}, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			if tenant == owner {
				if len(records) != 1 || records[0].Kind != "rbac.assignment" || records[0].ResourceID != credential.AdminID || records[0].Detail["role"] != credential.Role || records[0].TenantID != credential.TenantID {
					t.Fatalf("evidence does not match identity: %+v", records)
				}
			} else if len(records) != 0 {
				t.Fatalf("global accounts exported to %s: %+v", tenant, records)
			}
		}
		evidenceExec(t, store, `UPDATE tenants SET status = 'suspended' WHERE id = $1`, owner)
	}
	for _, tenant := range []string{"suspended", "first", "second"} {
		records, err := store.Evidence.Collect(ctx, tenancy.TenantScope{TenantID: tenant}, time.Time{}, time.Time{})
		if err != nil || len(records) != 0 {
			t.Fatalf("no active tenant should mean no local role evidence: %+v, %v", records, err)
		}
	}
}

func TestEvidenceDestinationTenantJoin(t *testing.T) {
	store := evidenceTestStore(t, "destinations")
	for _, tenant := range []string{"a", "b"} {
		evidenceExec(t, store, `INSERT INTO tenants(id, public_ref, name) VALUES ($1, $1, $1)`, tenant)
		evidenceExec(t, store, `INSERT INTO providers(id, tenant_id, owner_scope, name, type)
VALUES ($1, $1, 'TENANT_PRIVATE', $1, 'openai')`, tenant)
		evidenceExec(t, store, `INSERT INTO provider_credentials(id, provider_id, tenant_id, owner_scope, label, secret_ref, fingerprint)
VALUES ($1, $1, $1, 'TENANT_PRIVATE', 'credential', 'secret', 'fingerprint')`, tenant)
	}
	evidenceExec(t, store, `INSERT INTO providers(id, owner_scope, name, type) VALUES ('shared', 'SYSTEM_SHARED', 'shared', 'openai')`)
	evidenceExec(t, store, `INSERT INTO provider_credentials(id, provider_id, owner_scope, label, secret_ref, fingerprint)
VALUES ('shared', 'shared', 'SYSTEM_SHARED', 'credential', 'secret', 'fingerprint')`)
	// The schema permits foreign-tenant provider references. Neither side of
	// such a join may leak into the export, while shared providers are valid.
	for _, tenant := range []string{"a", "b"} {
		for _, provider := range []string{"a", "b", "shared"} {
			evidenceExec(t, store, `INSERT INTO model_deployments(id, tenant_id, provider_id, credential_id, upstream_model)
VALUES ($1, $2, $3, $3, 'model')`, tenant+"-"+provider, tenant, provider)
		}
	}
	for _, tenant := range []string{"a", "b"} {
		records, err := store.Evidence.Collect(context.Background(), tenancy.TenantScope{TenantID: tenant}, time.Time{}, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, record := range records {
			if record.Kind != "provider.destination" || record.TenantID != tenant {
				t.Fatalf("unexpected destination: %+v", record)
			}
			got[record.ResourceID] = true
		}
		want := map[string]bool{tenant + "-" + tenant: true, tenant + "-shared": true}
		if !reflect.DeepEqual(got, want) || len(records) != 2 {
			t.Fatalf("destination IDs = %v, want %v", got, want)
		}
	}
}
