package sqlrepo

import (
	"context"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/platform/storage/sqlite"
	"github.com/F31/liteAIG/internal/tenancy"
	"github.com/F31/liteAIG/migrations"
)

func a2aTaskTestStore(t *testing.T, name string) *Store {
	t.Helper()
	db, err := sqlite.Open(context.Background(), "file:a2atask-"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return Open(db, Deps{})
}

func seedA2ATaskTenant(t *testing.T, store *Store, tenant string) {
	t.Helper()
	if _, err := store.db.ExecContext(context.Background(),
		`INSERT INTO tenants(id, public_ref, name) VALUES ($1, $1, $1) ON CONFLICT DO NOTHING`, tenant); err != nil {
		t.Fatalf("seed a2a tenant %s: %v", tenant, err)
	}
}

func sampleA2ATask(taskID, tenant, key string) A2ATask {
	return A2ATask{
		TaskID: taskID, TenantID: tenant, ExternalAgentID: "agent-ext",
		ProjectID: "project-1", RequestID: taskID, IdempotencyKey: key,
		Status: A2ATaskPending, Message: "hello",
	}
}

func TestA2ATaskCreateGetUnique(t *testing.T) {
	store := a2aTaskTestStore(t, "crud")
	ctx := context.Background()
	tenantA, tenantB := "tenant-a", "tenant-b"
	seedA2ATaskTenant(t, store, tenantA)
	seedA2ATaskTenant(t, store, tenantB)
	scopeA := tenancy.TenantScope{TenantID: tenantA}

	task := sampleA2ATask("task-1", tenantA, "key-1")
	if err := store.A2ATask.Create(ctx, scopeA, task); err != nil {
		t.Fatal(err)
	}

	// GetByTask and GetByIdempotency both resolve the same durable task.
	got, ok, err := store.A2ATask.GetByTask(ctx, scopeA, "task-1")
	if err != nil || !ok {
		t.Fatalf("GetByTask = ok=%v err=%v", ok, err)
	}
	if got.Status != A2ATaskPending || got.Message != "hello" || got.IdempotencyKey != "key-1" || got.Hops != 0 || got.Calls != 0 || got.Attempts != 0 {
		t.Fatalf("GetByTask row = %+v", got)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not populated: %+v", got)
	}
	byKey, ok, err := store.A2ATask.GetByIdempotency(ctx, scopeA, "key-1")
	if err != nil || !ok || byKey.TaskID != "task-1" {
		t.Fatalf("GetByIdempotency = %+v ok=%v err=%v", byKey, ok, err)
	}

	// The unique (tenant_id, idempotency_key) constraint makes a second Create
	// with the same key a no-op: the first durable task wins unchanged.
	dup := sampleA2ATask("task-2", tenantA, "key-1")
	if err := store.A2ATask.Create(ctx, scopeA, dup); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.A2ATask.GetByTask(ctx, scopeA, "task-2"); ok {
		t.Fatal("duplicate idempotency key created a second task row")
	}
	still, _, _ := store.A2ATask.GetByIdempotency(ctx, scopeA, "key-1")
	if still.TaskID != "task-1" {
		t.Fatalf("idempotency key moved to another task: %+v", still)
	}

	// The same idempotency key is legal in another tenant.
	scopeB := tenancy.TenantScope{TenantID: tenantB}
	if err := store.A2ATask.Create(ctx, scopeB, sampleA2ATask("task-b", tenantB, "key-1")); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.A2ATask.GetByTask(ctx, scopeB, "task-b"); !ok {
		t.Fatal("tenant B task missing")
	}
	if _, ok, _ := store.A2ATask.GetByIdempotency(ctx, scopeB, "key-1"); !ok {
		t.Fatal("tenant B idempotency lookup failed")
	}

	// Missing rows and cross-tenant reads stay empty.
	if _, ok, _ := store.A2ATask.GetByTask(ctx, scopeA, "missing"); ok {
		t.Fatal("missing task resolved")
	}
	if _, ok, _ := store.A2ATask.GetByIdempotency(ctx, scopeB, "key-1"); !ok {
		t.Fatal("tenant B key should resolve")
	}
	if _, ok, _ := store.A2ATask.GetByTask(ctx, scopeA, "task-b"); ok {
		t.Fatal("cross-tenant GetByTask leaked")
	}
	if _, ok, _ := store.A2ATask.GetByIdempotency(ctx, scopeA, "key-1"); !ok {
		t.Fatal("tenant A key should still resolve")
	}

	// Invalid scope is rejected.
	if err := store.A2ATask.Create(ctx, tenancy.TenantScope{}, task); err == nil {
		t.Fatal("Create accepted an empty scope")
	}
	if _, _, err := store.A2ATask.GetByTask(ctx, tenancy.TenantScope{}, "task-1"); err == nil {
		t.Fatal("GetByTask accepted an empty scope")
	}
}

func TestA2ATaskAdvanceCountersAndLimits(t *testing.T) {
	store := a2aTaskTestStore(t, "advance")
	ctx := context.Background()
	tenant := "tenant-adv"
	seedA2ATaskTenant(t, store, tenant)
	scope := tenancy.TenantScope{TenantID: tenant}
	if err := store.A2ATask.Create(ctx, scope, sampleA2ATask("task-1", tenant, "key-1")); err != nil {
		t.Fatal(err)
	}

	// Admit stays within both limits, then the cumulative hop budget is spent.
	applied, err := store.A2ATask.AdvanceCounters(ctx, scope, "task-1", 2, 1, 3, 16)
	if err != nil || !applied {
		t.Fatalf("first advance applied=%v err=%v", applied, err)
	}
	applied, err = store.A2ATask.AdvanceCounters(ctx, scope, "task-1", 1, 1, 3, 16)
	if err != nil || !applied {
		t.Fatalf("second advance applied=%v err=%v", applied, err)
	}
	row, ok, _ := store.A2ATask.GetByTask(ctx, scope, "task-1")
	if !ok || row.Hops != 3 || row.Calls != 2 || row.Attempts != 2 {
		t.Fatalf("counters after admits = %+v", row)
	}

	// A third hop would exceed maxHops=3: refused and counters unchanged.
	applied, err = store.A2ATask.AdvanceCounters(ctx, scope, "task-1", 1, 1, 3, 16)
	if err != nil || applied {
		t.Fatalf("over-hop advance applied=%v err=%v", applied, err)
	}
	row, _, _ = store.A2ATask.GetByTask(ctx, scope, "task-1")
	if row.Hops != 3 || row.Calls != 2 || row.Attempts != 2 {
		t.Fatalf("counters moved on refused advance = %+v", row)
	}

	// Call-cap refusal: advance toward maxCalls=16 one call at a time until the
	// guard refuses, then assert the call budget is exhausted.
	for i := 0; i < 14; i++ {
		if applied, _ = store.A2ATask.AdvanceCounters(ctx, scope, "task-1", 0, 1, 3, 16); !applied {
			t.Fatalf("call advance %d unexpectedly refused", i)
		}
	}
	applied, err = store.A2ATask.AdvanceCounters(ctx, scope, "task-1", 0, 1, 3, 16)
	if err != nil || applied {
		t.Fatalf("over-call advance applied=%v err=%v", applied, err)
	}
	row, _, _ = store.A2ATask.GetByTask(ctx, scope, "task-1")
	if row.Calls != 16 || row.Attempts != 16 {
		t.Fatalf("final counters = %+v, want calls=16 attempts=16", row)
	}

	// A task in another tenant cannot be advanced under the wrong scope.
	seedA2ATaskTenant(t, store, "tenant-other")
	if err := store.A2ATask.Create(ctx, tenancy.TenantScope{TenantID: "tenant-other"}, sampleA2ATask("task-x", "tenant-other", "key-x")); err != nil {
		t.Fatal(err)
	}
	if applied, _ := store.A2ATask.AdvanceCounters(ctx, scope, "task-x", 1, 1, 3, 16); applied {
		t.Fatal("cross-tenant advance applied")
	}
	if _, ok, _ := store.A2ATask.GetByTask(ctx, scope, "task-x"); ok {
		t.Fatal("cross-tenant task visible")
	}
}

func TestA2ATaskTryStartAndStaleRecovery(t *testing.T) {
	store := a2aTaskTestStore(t, "start")
	ctx := context.Background()
	tenant := "tenant-start"
	seedA2ATaskTenant(t, store, tenant)
	scope := tenancy.TenantScope{TenantID: tenant}
	if err := store.A2ATask.Create(ctx, scope, sampleA2ATask("task-1", tenant, "key-1")); err != nil {
		t.Fatal(err)
	}

	staleBefore := time.Now().Add(-5 * time.Minute)
	started, err := store.A2ATask.TryStart(ctx, scope, "task-1", staleBefore)
	if err != nil || !started {
		t.Fatalf("first TryStart started=%v err=%v", started, err)
	}
	started, err = store.A2ATask.TryStart(ctx, scope, "task-1", staleBefore)
	if err != nil || started {
		t.Fatalf("concurrent TryStart started=%v err=%v, want false", started, err)
	}

	// Mark the running row as abandoned, reap it to failed, then claim it again.
	oldTimestamp := time.Now().Add(-10 * time.Minute).UTC().Format("2006-01-02 15:04:05")
	if _, err := store.db.ExecContext(ctx, `UPDATE a2a_tasks SET updated_at=$3 WHERE tenant_id=$1 AND task_id=$2`, tenant, "task-1", oldTimestamp); err != nil {
		t.Fatal(err)
	}
	reaped, err := store.A2ATask.ReapStaleRunning(ctx, scope, time.Now().Add(-5*time.Minute))
	if err != nil || reaped != 1 {
		t.Fatalf("ReapStaleRunning reaped=%d err=%v", reaped, err)
	}
	row, _, _ := store.A2ATask.GetByTask(ctx, scope, "task-1")
	if row.Status != A2ATaskFailed {
		t.Fatalf("reaped status = %q", row.Status)
	}
	started, err = store.A2ATask.TryStart(ctx, scope, "task-1", time.Now().Add(-5*time.Minute))
	if err != nil || !started {
		t.Fatalf("retry TryStart started=%v err=%v", started, err)
	}

	seedA2ATaskTenant(t, store, "tenant-other-start")
	if err := store.A2ATask.Create(ctx, tenancy.TenantScope{TenantID: "tenant-other-start"}, sampleA2ATask("task-x", "tenant-other-start", "key-x")); err != nil {
		t.Fatal(err)
	}
	if started, _ := store.A2ATask.TryStart(ctx, scope, "task-x", staleBefore); started {
		t.Fatal("cross-tenant TryStart applied")
	}
}

func TestA2ATaskStatusResult(t *testing.T) {
	store := a2aTaskTestStore(t, "status")
	ctx := context.Background()
	tenant := "tenant-status"
	seedA2ATaskTenant(t, store, tenant)
	scope := tenancy.TenantScope{TenantID: tenant}
	if err := store.A2ATask.Create(ctx, scope, sampleA2ATask("task-1", tenant, "key-1")); err != nil {
		t.Fatal(err)
	}

	if err := store.A2ATask.SetStatusResult(ctx, scope, "task-1", A2ATaskRunning, ""); err != nil {
		t.Fatal(err)
	}
	row, _, _ := store.A2ATask.GetByTask(ctx, scope, "task-1")
	if row.Status != A2ATaskRunning || row.Result != "" {
		t.Fatalf("running row = %+v", row)
	}

	if err := store.A2ATask.SetStatusResult(ctx, scope, "task-1", A2ATaskCompleted, "a2a-echo"); err != nil {
		t.Fatal(err)
	}
	row, _, _ = store.A2ATask.GetByTask(ctx, scope, "task-1")
	if row.Status != A2ATaskCompleted || row.Result != "a2a-echo" {
		t.Fatalf("completed row = %+v", row)
	}

	if err := store.A2ATask.SetStatusResult(ctx, scope, "task-1", A2ATaskFailed, ""); err != nil {
		t.Fatal(err)
	}
	row, _, _ = store.A2ATask.GetByTask(ctx, scope, "task-1")
	if row.Status != A2ATaskFailed || row.Result != "" {
		t.Fatalf("failed row = %+v", row)
	}

	// Status updates are tenant-scoped: updating a foreign task touches nothing.
	if err := store.A2ATask.SetStatusResult(ctx, scope, "missing", A2ATaskCompleted, "x"); err != nil {
		t.Fatalf("updating a missing task should be a silent no-op: %v", err)
	}
}
