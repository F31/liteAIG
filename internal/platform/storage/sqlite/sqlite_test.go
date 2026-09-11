package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	controlconfig "github.com/F31/liteAIG/internal/controlplane/config"
	"github.com/F31/liteAIG/internal/controlplane/setup"
	setupcontract "github.com/F31/liteAIG/internal/controlplane/setup/contracttest"
	"github.com/F31/liteAIG/internal/finops/accounting"
	"github.com/F31/liteAIG/internal/finops/budget"
	"github.com/F31/liteAIG/internal/identity/apikey"
	"github.com/F31/liteAIG/internal/identity/password"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/observability/alert"
	"github.com/F31/liteAIG/internal/organization"
	"github.com/F31/liteAIG/internal/platform/clock"
	"github.com/F31/liteAIG/internal/platform/id"
	"github.com/F31/liteAIG/internal/platform/secrets"
	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/internal/tenancy"
	"github.com/F31/liteAIG/internal/tenancy/contracttest"
	"github.com/F31/liteAIG/migrations"
	"strings"
	"testing"
	"time"
)

func TestMigrationsApplyIdempotently(t *testing.T) {
	db, err := Open(context.Background(), "file:migrations?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for i := 0; i < 2; i++ {
		if err := migrations.Apply(context.Background(), db); err != nil {
			t.Fatalf("Apply() pass %d error = %v", i+1, err)
		}
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if want := len(migrations.Versions()); count != want {
		t.Fatalf("schema_migrations count = %d, want %d", count, want)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM resource_provider_catalog").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count < 6 {
		t.Fatalf("provider catalog count = %d, want seeded providers", count)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM resource_model_catalog WHERE provider_id = 'deepseek'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("deepseek model catalog was not seeded")
	}
}

func TestAccountingFinalizationIsIdempotentAndScoped(t *testing.T) {
	db, err := Open(context.Background(), "file:accounting?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	tenantID := "90000000-0000-4000-8000-000000000001"
	projectID := "90000000-0000-4000-8000-000000000002"
	if _, err := db.Exec(`INSERT INTO tenants(id,public_ref,name) VALUES ($1,'account-ref','Account')`, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects(id,tenant_id,name) VALUES ($1,$2,'Default')`, projectID, tenantID); err != nil {
		t.Fatal(err)
	}
	repository := sqlrepo.NewAccountingRepository(db)
	cost := 0.01
	facts := accounting.Facts{UsageEventID: "90000000-0000-4000-8000-000000000003", RequestID: "90000000-0000-4000-8000-000000000004", TenantID: tenantID, ProjectID: projectID, LogicalModel: "chat", DeploymentID: "deployment", Outcome: "success", UsageSource: "api", InputTokens: 3, OutputTokens: 2, SnapshotVersion: 1, SecurityEpoch: 1, ProviderCost: &cost, ProviderCurrency: "USD", ReceivedAt: time.Unix(1, 0), CompletedAt: time.Unix(2, 0), SessionID: "session", TaskID: "task", RootTaskID: "root", ParentTaskID: "parent", AgentID: "agent"}
	created, err := repository.Finalize(context.Background(), facts)
	if err != nil || !created {
		t.Fatalf("Finalize()=%t,%v", created, err)
	}
	created, err = repository.Finalize(context.Background(), facts)
	if err != nil || created {
		t.Fatalf("duplicate Finalize()=%t,%v", created, err)
	}
	var usage int
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_events WHERE request_id=$1`, facts.RequestID).Scan(&usage); err != nil || usage != 1 {
		t.Fatalf("usage count=%d,%v", usage, err)
	}
	requestRecord, err := repository.GetRequest(context.Background(), tenancy.TenantScope{TenantID: tenantID}, facts.RequestID)
	if err != nil || requestRecord.SessionID != "session" || requestRecord.RootTaskID != "root" || requestRecord.AgentID != "agent" {
		t.Fatalf("request task linkage=%+v,%v", requestRecord, err)
	}
	usageRecord, err := repository.GetUsage(context.Background(), tenancy.TenantScope{TenantID: tenantID}, facts.RequestID)
	if err != nil || usageRecord.TaskID != "task" || usageRecord.ParentTaskID != "parent" {
		t.Fatalf("usage task linkage=%+v,%v", usageRecord, err)
	}
	if _, err := repository.GetRequest(context.Background(), tenancy.TenantScope{TenantID: "other"}, facts.RequestID); !errors.Is(err, tenancy.ErrNotFound) {
		t.Fatalf("cross-tenant GetRequest() error=%v", err)
	}
	if _, err := repository.GetUsage(context.Background(), tenancy.TenantScope{TenantID: "other"}, facts.RequestID); !errors.Is(err, tenancy.ErrNotFound) {
		t.Fatalf("cross-tenant GetUsage() error=%v", err)
	}
	rows, err := db.Query(`PRAGMA table_info(request_records)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notnull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(name)
		if strings.Contains(lower, "prompt") || strings.Contains(lower, "response_body") {
			t.Fatalf("sensitive content column present: %s", name)
		}
	}
}

type testSecrets map[string][]byte

func (s testSecrets) Resolve(_ context.Context, ref string) ([]byte, error) {
	value, ok := s[ref]
	if !ok {
		return nil, errors.New("secret not found")
	}
	return value, nil
}

func TestVirtualKeyPersistsOnlyDigestAndMetadata(t *testing.T) {
	db, err := Open(context.Background(), "file:api-key?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	tenantID := "70000000-0000-4000-8000-000000000001"
	projectID := "70000000-0000-4000-8000-000000000002"
	tenantRef := "11111111111111111111111111111111"
	if _, err := db.Exec(`INSERT INTO tenants(id,public_ref,name) VALUES ($1,$2,'Key Tenant')`, tenantID, tenantRef); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects(id,tenant_id,name) VALUES ($1,$2,'Default Project')`, projectID, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO key_pepper_versions(version,pepper_ref,status) VALUES (1,'secret://pepper/1','active')`); err != nil {
		t.Fatal(err)
	}
	repository := sqlrepo.NewAPIKeyRepository(db)
	cipher, err := secrets.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	service, err := apikey.NewService(repository, testSecrets{"secret://pepper/1": []byte("pepper-value")}, apikey.NewGenerator(bytes.NewReader(make([]byte, 48))), id.NewGenerator(bytes.NewReader(make([]byte, 16))), clock.System{}, cipher)
	if err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Now().Add(time.Hour)
	result, err := service.Create(context.Background(), tenancy.TenantScope{TenantID: tenantID}, apikey.CreateInput{TenantRef: tenantRef, ProjectID: projectID, Name: "first-key", ExpiresAt: &expiresAt, ModelAllowlist: []string{"default-chat"}, IPAllowlist: []string{"10.0.0.0/8"}})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := apikey.Parse(result.Key)
	if err != nil {
		t.Fatal(err)
	}
	records, err := repository.ListActive(context.Background(), tenancy.TenantScope{TenantID: tenantID})
	if err != nil || len(records) != 1 {
		t.Fatalf("ListActive()=%+v,%v", records, err)
	}
	if !apikey.Verify([]byte("pepper-value"), parsed, records[0].HMACDigest) {
		t.Fatal("persisted digest did not verify")
	}
	if records[0].ExpiresAt == nil {
		t.Fatal("expiration was not persisted")
	}
	other, err := repository.ListActive(context.Background(), tenancy.TenantScope{TenantID: "99999999-0000-4000-8000-000000000001"})
	if err != nil || len(other) != 0 {
		t.Fatalf("cross-tenant ListActive() = %+v, %v", other, err)
	}
	tenantB := "70000000-0000-4000-8000-000000000004"
	projectB := "70000000-0000-4000-8000-000000000005"
	if _, err := db.Exec(`INSERT INTO tenants(id,public_ref,name) VALUES ($1,'other-key-ref','Other')`, tenantB); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects(id,tenant_id,name) VALUES ($1,$2,'Other Project')`, projectB, tenantB); err != nil {
		t.Fatal(err)
	}
	cross := result.Record
	cross.ID = "70000000-0000-4000-8000-000000000006"
	cross.PublicID = "66666666666666666666666666666666"
	cross.ProjectID = projectB
	if err := repository.Create(context.Background(), tenancy.TenantScope{TenantID: tenantID}, cross); !errors.Is(err, tenancy.ErrScopeMismatch) {
		t.Fatalf("cross-tenant project Create() error=%v", err)
	}
	var publicID, fingerprint, models, ips string
	var ciphertext []byte
	if err := db.QueryRow(`SELECT public_id,fingerprint,model_allowlist,ip_allowlist,key_ciphertext FROM api_keys`).Scan(&publicID, &fingerprint, &models, &ips, &ciphertext); err != nil {
		t.Fatal(err)
	}
	for _, stored := range []string{publicID, fingerprint, models, ips, string(ciphertext)} {
		if strings.Contains(stored, parsed.Secret) || strings.Contains(stored, result.Key) {
			t.Fatalf("database leaked full key or secret: %q", stored)
		}
	}
	if len(ciphertext) == 0 {
		t.Fatal("key ciphertext was not persisted")
	}
	revealed, err := service.Reveal(context.Background(), tenancy.TenantScope{TenantID: tenantID}, result.Record.ID)
	if err != nil {
		t.Fatalf("Reveal() = %v", err)
	}
	if revealed != result.Key {
		t.Fatalf("Reveal() = %q, want %q", revealed, result.Key)
	}
	list, err := repository.List(context.Background(), tenancy.TenantScope{TenantID: tenantID})
	if err != nil || len(list) != 1 {
		t.Fatalf("List()=%+v,%v", list, err)
	}
	if len(list[0].KeyCiphertext) == 0 {
		t.Fatal("List() did not return key ciphertext")
	}
	if _, err := repository.Get(context.Background(), tenancy.TenantScope{TenantID: "99999999-0000-4000-8000-000000000001"}, result.Record.ID); !errors.Is(err, tenancy.ErrNotFound) {
		t.Fatalf("cross-tenant Get() error = %v, want ErrNotFound", err)
	}
}

func TestConfigDraftOptimisticConcurrency(t *testing.T) {
	db, err := Open(context.Background(), "file:config-draft?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	tenantID := "30000000-0000-4000-8000-000000000001"
	if _, err := db.Exec(`INSERT INTO tenants(id, public_ref, name) VALUES ($1, 'draft-ref', 'Draft Tenant')`, tenantID); err != nil {
		t.Fatal(err)
	}
	repository := sqlrepo.NewConfigRepository(db)
	scope := tenancy.TenantScope{TenantID: tenantID}
	draft := controlconfig.Draft{
		ID:          "30000000-0000-4000-8000-000000000002",
		TenantID:    tenantID,
		BaseVersion: 0,
		Revision:    1,
		Status:      "editing",
		Config: controlconfig.TenantConfig{
			SchemaVersion: controlconfig.SchemaV1,
			Tenant:        controlconfig.TenantResource{ID: tenantID, PublicRef: "draft-ref", Status: "active"},
		},
		CreatedBy: "30000000-0000-4000-8000-000000000003",
		UpdatedBy: "30000000-0000-4000-8000-000000000003",
		CreatedAt: time.Unix(1, 0),
		UpdatedAt: time.Unix(1, 0),
	}
	if err := repository.CreateDraft(context.Background(), scope, draft); err != nil {
		t.Fatal(err)
	}
	draft.Config.Tenant.SecurityEpoch = 2
	updated, err := repository.UpdateDraft(context.Background(), scope, draft.ID, 1, draft.Config, draft.UpdatedBy)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 || updated.Config.Tenant.SecurityEpoch != 2 {
		t.Fatalf("updated draft = %+v", updated)
	}
	if _, err := repository.UpdateDraft(context.Background(), scope, draft.ID, 1, draft.Config, draft.UpdatedBy); !errors.Is(err, controlconfig.ErrRevisionConflict) {
		t.Fatalf("stale UpdateDraft() error = %v", err)
	}
	if _, err := repository.GetDraft(context.Background(), tenancy.TenantScope{TenantID: "40000000-0000-4000-8000-000000000001"}, draft.ID); !errors.Is(err, controlconfig.ErrDraftNotFound) {
		t.Fatalf("cross-tenant GetDraft() error = %v", err)
	}
}

func TestConfigPublishRollbackAndControlPlaneOutage(t *testing.T) {
	db, err := Open(context.Background(), "file:config-lifecycle?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	tenantID := "50000000-0000-4000-8000-000000000001"
	if _, err := db.Exec(`INSERT INTO tenants(id, public_ref, name) VALUES ($1, 'runtime-ref', 'Runtime Tenant')`, tenantID); err != nil {
		t.Fatal(err)
	}
	repository := sqlrepo.NewConfigRepository(db)
	document := lifecycleConfig(tenantID)
	draft := controlconfig.Draft{
		ID: "50000000-0000-4000-8000-000000000002", TenantID: tenantID, Revision: 1, Status: "editing", Config: document,
		CreatedBy: "50000000-0000-4000-8000-000000000003", UpdatedBy: "50000000-0000-4000-8000-000000000003", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	scope := tenancy.TenantScope{TenantID: tenantID}
	if err := repository.CreateDraft(context.Background(), scope, draft); err != nil {
		t.Fatal(err)
	}
	registry := &runtime.ActiveRegistry{}
	service := controlconfig.NewService(repository, sqlrepo.NewAPIKeyRepository(db), registry, id.NewGenerator(nil), clock.System{})
	version, diagnostics, err := service.Publish(context.Background(), scope, draft.ID, 1, draft.CreatedBy)
	if err != nil || controlconfig.HasErrors(diagnostics) {
		t.Fatalf("Publish() = %+v, %+v, %v", version, diagnostics, err)
	}
	active, ok := registry.Tenant("runtime-ref")
	if !ok || active.Version != 1 {
		t.Fatalf("active snapshot = %+v", active)
	}
	rolledBack, err := service.Rollback(context.Background(), scope, 1, draft.CreatedBy)
	if err != nil || rolledBack.Version != 2 {
		t.Fatalf("Rollback() = %+v, %v", rolledBack, err)
	}
	invalid := lifecycleConfig(tenantID)
	invalid.Credentials[0].SecretRef = "raw-provider-secret"
	invalidDraft := draft
	invalidDraft.ID = "50000000-0000-4000-8000-000000000006"
	invalidDraft.Config = invalid
	if err := repository.CreateDraft(context.Background(), scope, invalidDraft); err != nil {
		t.Fatal(err)
	}
	if _, diagnostics, err := service.Publish(context.Background(), scope, invalidDraft.ID, 1, draft.CreatedBy); err == nil || !controlconfig.HasErrors(diagnostics) {
		t.Fatalf("invalid Publish() diagnostics=%+v error=%v", diagnostics, err)
	}
	stillActive, _ := registry.Tenant("runtime-ref")
	if stillActive.Version != 2 {
		t.Fatalf("invalid publish replaced active snapshot: %+v", stillActive)
	}
	var auditDetails string
	if err := db.QueryRow(`SELECT details FROM audit_events WHERE action='config.publish'`).Scan(&auditDetails); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(auditDetails, "sensitive-provider-secret") {
		t.Fatalf("audit leaked secret: %s", auditDetails)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	loaded, ok := registry.Tenant("runtime-ref")
	if !ok || loaded.Version != 2 {
		t.Fatalf("loaded snapshot unavailable after Control Plane close: %+v", loaded)
	}
}

func TestConfigReconcileRestoresPublishedVersion(t *testing.T) {
	db, err := Open(context.Background(), "file:config-reconcile?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	tenantID := "a2000000-0000-4000-8000-000000000001"
	if _, err := db.Exec(`INSERT INTO tenants(id, public_ref, name) VALUES ($1, 'reconcile-ref', 'Reconcile')`, tenantID); err != nil {
		t.Fatal(err)
	}
	repository := sqlrepo.NewConfigRepository(db)
	document := lifecycleConfig(tenantID)
	document.Tenant.PublicRef = "reconcile-ref"
	draft := controlconfig.Draft{
		ID: "a2000000-0000-4000-8000-000000000002", TenantID: tenantID, Revision: 1, Status: "editing", Config: document,
		CreatedBy: "a2000000-0000-4000-8000-000000000003", UpdatedBy: "a2000000-0000-4000-8000-000000000003", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	scope := tenancy.TenantScope{TenantID: tenantID}
	if err := repository.CreateDraft(context.Background(), scope, draft); err != nil {
		t.Fatal(err)
	}
	registry := &runtime.ActiveRegistry{}
	service := controlconfig.NewService(repository, sqlrepo.NewAPIKeyRepository(db), registry, id.NewGenerator(nil), clock.System{})
	version, _, err := service.Publish(context.Background(), scope, draft.ID, 1, draft.CreatedBy)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate drift: a divergent snapshot is active.
	drifted := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{TenantID: tenantID, TenantRef: "reconcile-ref", Version: 99})
	registry.ActivateTenant("reconcile-ref", drifted)
	active, _ := registry.Tenant("reconcile-ref")
	if active.Version != 99 {
		t.Fatalf("drift not established: %+v", active)
	}

	// Reconcile restores the published version without creating new history.
	reconciled, err := service.Reconcile(context.Background(), scope, version.Version, draft.CreatedBy)
	if err != nil {
		t.Fatal(err)
	}
	restored, ok := registry.Tenant("reconcile-ref")
	if !ok || restored.Version != version.Version {
		t.Fatalf("reconcile did not restore version %d: %+v", version.Version, restored)
	}
	if reconciled.Version != version.Version {
		t.Fatalf("reconciled version = %d", reconciled.Version)
	}
	var versions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM config_versions`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 1 {
		t.Fatalf("reconcile created new history: %d versions", versions)
	}
	var audits int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE action='config.reconcile'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("reconcile audit count = %d", audits)
	}
}

func TestSystemConfigDefaultsAppliedOnPublish(t *testing.T) {
	db, err := Open(context.Background(), "file:system-config?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	tenantID := "b3000000-0000-4000-8000-000000000001"
	if _, err := db.Exec(`INSERT INTO tenants(id, public_ref, name) VALUES ($1, 'system-defaults-ref', 'System Defaults')`, tenantID); err != nil {
		t.Fatal(err)
	}
	repository := sqlrepo.NewConfigRepository(db)
	systemRepo := sqlrepo.NewSystemConfigRepository(db)
	document := lifecycleConfig(tenantID)
	document.Tenant.PublicRef = "system-defaults-ref"
	document.Tenant.AllowedDataRegions = nil
	document.Tenant.ResidencyEnforcement = ""
	document.Projects[0].AllowedDataRegions = nil
	document.Projects[0].ResidencyEnforcement = ""
	document.Deployments[0].DataRegion = "eu"
	draft := controlconfig.Draft{
		ID: "b3000000-0000-4000-8000-000000000002", TenantID: tenantID, Revision: 1, Status: "editing", Config: document,
		CreatedBy: "b3000000-0000-4000-8000-000000000003", UpdatedBy: "b3000000-0000-4000-8000-000000000003", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	scope := tenancy.TenantScope{TenantID: tenantID}
	if err := repository.CreateDraft(context.Background(), scope, draft); err != nil {
		t.Fatal(err)
	}
	system := controlconfig.SystemConfig{TenantDefaults: controlconfig.TenantPolicyDefaults{AllowedDataRegions: []string{"eu"}, ResidencyEnforcement: "strict"}}
	if _, err := systemRepo.SetSystemConfig(context.Background(), system, draft.CreatedBy); err != nil {
		t.Fatal(err)
	}
	registry := &runtime.ActiveRegistry{}
	events := &recordingDomainEvents{}
	service := controlconfig.NewService(repository, sqlrepo.NewAPIKeyRepository(db), registry, id.NewGenerator(nil), clock.System{})
	service.SetSystemRepository(systemRepo)
	service.SetEventSink(events)
	version, diagnostics, err := service.Publish(context.Background(), scope, draft.ID, 1, draft.CreatedBy)
	if err != nil || controlconfig.HasErrors(diagnostics) {
		t.Fatalf("Publish() = %+v, %+v, %v", version, diagnostics, err)
	}
	active, ok := registry.Tenant("system-defaults-ref")
	if !ok {
		t.Fatal("active snapshot missing")
	}
	project, ok := active.Project("project")
	if !ok || project.ResidencyEnforcement != "strict" || len(project.AllowedDataRegions) != 1 || project.AllowedDataRegions[0] != "eu" {
		t.Fatalf("system defaults not applied to project: %+v", project)
	}
	// The persisted SystemConfig must round-trip through the Service accessor.
	loaded, err := service.SystemConfig(context.Background())
	if err != nil || len(loaded.TenantDefaults.AllowedDataRegions) != 1 || loaded.TenantDefaults.AllowedDataRegions[0] != "eu" {
		t.Fatalf("SystemConfig() = %+v, %v", loaded, err)
	}
	if _, err := service.SetSystemConfig(context.Background(), controlconfig.SystemConfig{TenantDefaults: controlconfig.TenantPolicyDefaults{ResidencyEnforcement: "bogus"}}, draft.CreatedBy); err == nil {
		t.Fatal("invalid residency_enforcement accepted")
	}
}

func TestSystemConfigChangeReconcilesAllTenants(t *testing.T) {
	db, err := Open(context.Background(), "file:system-config-reconcile?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	repository := sqlrepo.NewConfigRepository(db)
	systemRepo := sqlrepo.NewSystemConfigRepository(db)
	registry := &runtime.ActiveRegistry{}
	events := &recordingDomainEvents{}
	service := controlconfig.NewService(repository, sqlrepo.NewAPIKeyRepository(db), registry, id.NewGenerator(nil), clock.System{})
	service.SetSystemRepository(systemRepo)
	service.SetEventSink(events)

	for i, ref := range []string{"tenant-a-ref", "tenant-b-ref"} {
		tenantID := fmt.Sprintf("c4000000-0000-4000-8000-00000000000%d", i+1)
		if _, err := db.Exec(`INSERT INTO tenants(id, public_ref, name) VALUES ($1, $2, 'Tenant')`, tenantID, ref); err != nil {
			t.Fatal(err)
		}
		document := lifecycleConfig(tenantID)
		document.Tenant.PublicRef = ref
		document.Tenant.AllowedDataRegions = nil
		document.Tenant.ResidencyEnforcement = ""
		document.Projects[0].AllowedDataRegions = nil
		document.Projects[0].ResidencyEnforcement = ""
		document.Deployments[0].DataRegion = "eu"
		draft := controlconfig.Draft{
			ID: fmt.Sprintf("c4000000-0000-4000-8000-0000000000%d0", i+1), TenantID: tenantID, Revision: 1, Status: "editing", Config: document,
			CreatedBy: tenantID, UpdatedBy: tenantID, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}
		scope := tenancy.TenantScope{TenantID: tenantID}
		if err := repository.CreateDraft(context.Background(), scope, draft); err != nil {
			t.Fatal(err)
		}
		if _, diagnostics, err := service.Publish(context.Background(), scope, draft.ID, 1, draft.CreatedBy); err != nil || controlconfig.HasErrors(diagnostics) {
			t.Fatalf("Publish(%s) = %+v, %v", ref, diagnostics, err)
		}
		active, ok := registry.Tenant(ref)
		project, hasProject := active.Project("project")
		if !ok || !hasProject || len(project.AllowedDataRegions) != 0 {
			t.Fatalf("tenant %s active before system defaults: %+v", ref, active)
		}
	}

	// Setting the system config must re-activate both published tenants with
	// the new System-to-Tenant defaults.
	if _, err := service.SetSystemConfig(context.Background(), controlconfig.SystemConfig{TenantDefaults: controlconfig.TenantPolicyDefaults{AllowedDataRegions: []string{"eu"}, ResidencyEnforcement: "strict"}}, "c4000000-0000-4000-8000-000000000001"); err != nil {
		t.Fatal(err)
	}
	if len(events.events) != 1 || events.events[0].Kind != "system_config.update" || events.events[0].Attributes["severity"] != "medium" || events.events[0].Attributes["actor_id"] == "" {
		t.Fatalf("system config events = %+v", events.events)
	}
	for _, ref := range []string{"tenant-a-ref", "tenant-b-ref"} {
		active, ok := registry.Tenant(ref)
		project, hasProject := active.Project("project")
		if !ok || !hasProject || project.ResidencyEnforcement != "strict" || len(project.AllowedDataRegions) != 1 || project.AllowedDataRegions[0] != "eu" {
			t.Fatalf("tenant %s active after system defaults: %+v", ref, active)
		}
	}
}

type recordingDomainEvents struct{ events []contracts.DomainEvent }

func (s *recordingDomainEvents) Emit(_ context.Context, event contracts.DomainEvent) error {
	s.events = append(s.events, event)
	return nil
}

func TestSystemAuditRecorderRecordsGlobalEvent(t *testing.T) {
	db, err := Open(context.Background(), "file:system-audit?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	actorID := "c5000000-0000-4000-8000-000000000001"
	recorder := sqlrepo.NewAuditRecorder(db, id.NewGenerator(nil))
	if err := recorder.RecordSystem(context.Background(), actorID, "system_config.update", "system_config", "1"); err != nil {
		t.Fatal(err)
	}
	var scope, actor, action, resourceType, resourceID string
	var tenantID sql.NullString
	if err := db.QueryRow(`SELECT tenant_id, scope, actor_id, action, resource_type, resource_id FROM audit_events WHERE action=$1`, "system_config.update").Scan(&tenantID, &scope, &actor, &action, &resourceType, &resourceID); err != nil {
		t.Fatal(err)
	}
	if tenantID.Valid || scope != "system" || actor != actorID || action != "system_config.update" || resourceType != "system_config" || resourceID != "1" {
		t.Fatalf("system audit event = tenant:%v scope:%s actor:%s action:%s resource:%s/%s", tenantID, scope, actor, action, resourceType, resourceID)
	}
}

func lifecycleConfig(tenantID string) controlconfig.TenantConfig {
	return controlconfig.TenantConfig{
		SchemaVersion: controlconfig.SchemaV1,
		Tenant:        controlconfig.TenantResource{ID: tenantID, PublicRef: "runtime-ref", Status: "active", SecurityEpoch: 1},
		Projects:      []controlconfig.Project{{ID: "project", TenantID: tenantID, Status: "active", AllowedDataRegions: []string{"us"}, ResidencyEnforcement: "strict"}},
		Providers:     []controlconfig.Provider{{ID: "provider", TenantID: tenantID, OwnerScope: "TENANT_PRIVATE", Type: "openai", Status: "enabled"}},
		Credentials:   []controlconfig.Credential{{ID: "credential", ProviderID: "provider", TenantID: tenantID, OwnerScope: "TENANT_PRIVATE", SecretRef: "secret://tenant/sensitive-provider-secret", Status: "enabled"}},
		Deployments:   []controlconfig.Deployment{{ID: "deployment", TenantID: tenantID, ProviderID: "provider", CredentialID: "credential", UpstreamModel: "model", DataRegion: "us", Status: "enabled"}},
		RoutePolicies: []controlconfig.RoutePolicy{{ID: "route", TenantID: tenantID, ProjectID: "project", Strategy: "priority", DeploymentIDs: []string{"deployment"}, Version: 1}},
		LogicalModels: []controlconfig.LogicalModel{{ID: "logical", TenantID: tenantID, Alias: "default-chat", RoutePolicyID: "route"}},
	}
}

func TestRepositoryConformance(t *testing.T) {
	db, err := Open(context.Background(), "file:repository?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	contracttest.Run(t, db, sqlrepo.NewTenancyRepository(db), sqlrepo.NewTenancyRepository(db))
	contracttest.RunTenantAdmin(t, sqlrepo.NewTenancyRepository(db))
}

func TestBootstrapRepositoryConformance(t *testing.T) {
	db, err := Open(context.Background(), "file:bootstrap-repository?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	setupcontract.Run(t, db, sqlrepo.NewBootstrapRepository(db))
}

func TestBootstrapServiceStoresOnlyArgon2idHash(t *testing.T) {
	db, err := Open(context.Background(), "file:bootstrap-service?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	hasher, err := password.NewArgon2id(password.Argon2idParams{
		MemoryKiB:   64,
		Iterations:  1,
		Parallelism: 1,
		SaltBytes:   16,
		KeyBytes:    32,
	}, bytes.NewReader(make([]byte, 16)))
	if err != nil {
		t.Fatal(err)
	}
	service, err := setup.NewService(
		setup.Config{DefaultProjectName: "Default Project", SettlementCurrency: "USD", MinimumPasswordSize: 12},
		sqlrepo.NewBootstrapRepository(db),
		hasher,
		id.NewGenerator(nil),
		id.NewGenerator(nil),
		clock.System{},
	)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("bootstrap-password")
	if _, err := service.Bootstrap(context.Background(), setup.Input{
		Username:   "admin",
		Password:   plaintext,
		TenantName: "Default Tenant",
	}); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := db.QueryRow("SELECT password_hash FROM local_admins").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, "$argon2id$") || strings.Contains(stored, string(plaintext)) {
		t.Fatalf("stored credential is not a safe Argon2id hash: %q", stored)
	}
}

func TestBudgetAuditRepositoryRecordsReservationLifecycle(t *testing.T) {
	db, err := Open(context.Background(), "file:budget-audit?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	tenantID := "a1000000-0000-4000-8000-000000000001"
	if _, err := db.Exec(`INSERT INTO tenants(id, public_ref, name) VALUES ($1, 'audit-ref', 'Audit')`, tenantID); err != nil {
		t.Fatal(err)
	}
	repository := sqlrepo.NewBudgetAuditRepository(db)
	event := budget.AuditEvent{
		ID: "a1000000-0000-4000-8000-000000000002", TenantID: tenantID, ReservationID: "reservation-1",
		ScopeType: "project", ScopeID: "project-1", Estimate: 12.5, Status: "reserved",
		WindowKeys: []string{"{tenant:t}:project:p:1d"}, CreatedAt: time.Unix(1, 0),
	}
	if err := repository.Record(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	var status string
	var estimate float64
	if err := db.QueryRow(`SELECT status, estimate FROM budget_reservations WHERE reservation_id=$1`, "reservation-1").Scan(&status, &estimate); err != nil {
		t.Fatal(err)
	}
	if status != "reserved" || estimate != 12.5 {
		t.Fatalf("record = %s %f", status, estimate)
	}
}

func TestOrganizationEffectiveDatedReassignmentPreservesHistory(t *testing.T) {
	db, err := Open(context.Background(), "file:organization?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	tenantID := "a3000000-0000-4000-8000-000000000001"
	userID := "a3000000-0000-4000-8000-000000000002"
	orgA := "a3000000-0000-4000-8000-000000000003"
	orgB := "a3000000-0000-4000-8000-000000000004"
	costA := "a3000000-0000-4000-8000-000000000005"
	if _, err := db.Exec(`INSERT INTO tenants(id, public_ref, name) VALUES ($1, 'org-ref', 'Org')`, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users(id, display_name) VALUES ($1, 'Alice')`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tenant_memberships(tenant_id, user_id) VALUES ($1, $2)`, tenantID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cost_centers(id, tenant_id, code, name) VALUES ($1, $2, 'cc-a', 'CC A')`, costA, tenantID); err != nil {
		t.Fatal(err)
	}
	repository := sqlrepo.NewOrganizationRepository(db)
	scope := tenancy.TenantScope{TenantID: tenantID}
	now := time.Unix(1000, 0)
	for _, unit := range []organization.OrgUnit{
		{ID: orgA, TenantID: tenantID, Name: "Org A", Path: "/a", CostCenterID: costA, Status: "active", CreatedAt: now},
		{ID: orgB, TenantID: tenantID, Name: "Org B", Path: "/b", Status: "active", CreatedAt: now},
	} {
		if err := repository.CreateOrgUnit(context.Background(), scope, unit); err != nil {
			t.Fatal(err)
		}
	}

	// Effective-dated reassignment: A until t=1100, then B.
	firstEnd := time.Unix(1100, 0)
	if err := repository.AssignUser(context.Background(), scope, organization.UserOrgAssignment{
		ID: "a3000000-0000-4000-8000-000000000006", TenantID: tenantID, UserID: userID, OrgUnitID: orgA,
		IsPrimary: true, ValidFrom: now, ValidTo: &firstEnd, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.AssignUser(context.Background(), scope, organization.UserOrgAssignment{
		ID: "a3000000-0000-4000-8000-000000000007", TenantID: tenantID, UserID: userID, OrgUnitID: orgB,
		IsPrimary: true, ValidFrom: firstEnd, ValidTo: nil, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	resolver := organization.NewResolver(repository, func(string) string { return "verified" })
	early := resolver.Resolve(context.Background(), scope, userID, time.Unix(1050, 0))
	late := resolver.Resolve(context.Background(), scope, userID, time.Unix(1200, 0))
	if early.OrgUnitID != orgA || early.CostCenterID != costA || early.OrgPath != "/a" {
		t.Fatalf("early attribution = %+v", early)
	}
	if late.OrgUnitID != orgB || late.OrgPath != "/b" {
		t.Fatalf("late attribution = %+v", late)
	}

	// History is preserved: both assignments remain.
	history, err := repository.ListAssignments(context.Background(), scope, userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("history = %d assignments", len(history))
	}
}

func TestOrganizationUntrustedLabelExcludedFromAttribution(t *testing.T) {
	db, err := Open(context.Background(), "file:org-untrusted?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	tenantID := "a4000000-0000-4000-8000-000000000001"
	userID := "a4000000-0000-4000-8000-000000000002"
	orgA := "a4000000-0000-4000-8000-000000000003"
	if _, err := db.Exec(`INSERT INTO tenants(id, public_ref, name) VALUES ($1, 'untrusted-ref', 'Untrusted')`, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users(id, display_name) VALUES ($1, 'Bob')`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tenant_memberships(tenant_id, user_id) VALUES ($1, $2)`, tenantID, userID); err != nil {
		t.Fatal(err)
	}
	repository := sqlrepo.NewOrganizationRepository(db)
	scope := tenancy.TenantScope{TenantID: tenantID}
	now := time.Unix(1000, 0)
	if err := repository.CreateOrgUnit(context.Background(), scope, organization.OrgUnit{ID: orgA, TenantID: tenantID, Name: "Org A", Path: "/a", Status: "active", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := repository.AssignUser(context.Background(), scope, organization.UserOrgAssignment{
		ID: "a4000000-0000-4000-8000-000000000004", TenantID: tenantID, UserID: userID, OrgUnitID: orgA,
		IsPrimary: true, ValidFrom: now, ValidTo: nil, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	resolver := organization.NewResolver(repository, func(string) string { return "untrusted" })
	attribution := resolver.Resolve(context.Background(), scope, userID, now)
	if attribution.AttributionTrust != "untrusted" {
		t.Fatalf("trust = %s", attribution.AttributionTrust)
	}
	// Untrusted labels must not feed User/OrgUnit budget or chargeback, regardless
	// of an existing assignment; the attribution still carries the org for
	// diagnostics, but the budget gate (orgbudget) ignores it via trust.
	if _, ok := (budget.OrgBudgetGate{TenantTag: "{tenant:x}"}).UserOrgWindowKeys(attribution, 24); ok {
		t.Fatal("untrusted attribution enabled User/OrgUnit budget windows")
	}
}

func TestAlertLifecyclePersistenceAndAudit(t *testing.T) {
	db, err := Open(context.Background(), "file:alert-lifecycle?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	tenantID := "a6000000-0000-4000-8000-000000000001"
	if _, err := db.Exec(`INSERT INTO tenants(id, public_ref, name) VALUES ($1, 'alert-ref', 'Alert')`, tenantID); err != nil {
		t.Fatal(err)
	}
	store := sqlrepo.NewAlertStore(db)
	auditor := sqlrepo.NewAlertAuditRecorder(db, id.NewGenerator(nil))
	service, err := alert.NewService(store, nil, auditor, id.NewGenerator(nil), clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	scope := tenancy.TenantScope{TenantID: tenantID}
	rule := alert.Rule{ID: "a6000000-0000-4000-8000-000000000002", TenantID: tenantID, Name: "budget-alert", RuleType: "budget", Metric: "budget.usage", Operator: "gt", Threshold: 100, WindowSeconds: 3600, Severity: alert.SeverityCritical, Enabled: true, CreatedBy: "admin"}
	if err := store.CreateRule(context.Background(), scope, rule); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	result, fired, err := service.Evaluate(context.Background(), scope, rule, 150, now.Add(-time.Hour), now)
	if err != nil || !fired {
		t.Fatalf("evaluate = %+v fired=%t err=%v", result, fired, err)
	}
	if err := service.Ack(context.Background(), scope, result.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	alerts, err := store.List(context.Background(), scope, alert.StatusAcknowledged)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 || alerts[0].ID != result.ID {
		t.Fatalf("alerts = %+v", alerts)
	}
	// Cross-tenant lookup must be denied.
	if _, err := store.Get(context.Background(), tenancy.TenantScope{TenantID: "other"}, result.ID); err != tenancy.ErrNotFound {
		t.Fatalf("cross-tenant alert read error = %v", err)
	}
	var audits int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE action='alert.ack'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("ack audit count = %d", audits)
	}
}
