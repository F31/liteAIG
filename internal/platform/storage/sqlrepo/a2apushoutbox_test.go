package sqlrepo

import (
	"context"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/federation"
	"github.com/F31/liteAIG/internal/tenancy"
)

func seedPushTask(t *testing.T, store *Store, tenant, taskID string) tenancy.TenantScope {
	t.Helper()
	seedA2ATaskTenant(t, store, tenant)
	scope := tenancy.TenantScope{TenantID: tenant}
	if err := store.A2ATask.Create(context.Background(), scope, sampleA2ATask(taskID, tenant, "key-"+taskID)); err != nil {
		t.Fatal(err)
	}
	return scope
}

func pushStatus(t *testing.T, store *Store, id string) string {
	t.Helper()
	var status string
	if err := store.db.QueryRowContext(context.Background(), `SELECT status FROM a2a_push_outbox WHERE id=$1`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

func TestA2APushOutboxClaimsRowsAtomically(t *testing.T) {
	store := a2aTaskTestStore(t, "push-claim")
	ctx := context.Background()
	seedPushTask(t, store, "tenant-push", "task-1")
	delivery := federation.A2APushDelivery{ID: "push-1", TaskID: "task-1", CallbackURL: "http://callback.test/hook", Payload: []byte(`{"ok":true}`), MaxAttempts: 2}
	if err := store.A2APushOutbox.EnqueueForTask(ctx, delivery); err != nil {
		t.Fatal(err)
	}

	first, err := store.A2APushOutbox.Due(ctx, 10, time.Now())
	if err != nil || len(first) != 1 || first[0].ID != "push-1" {
		t.Fatalf("first Due = %+v err=%v", first, err)
	}
	if status := pushStatus(t, store, "push-1"); status != "sending" {
		t.Fatalf("status after claim = %q", status)
	}
	second, err := store.A2APushOutbox.Due(ctx, 10, time.Now())
	if err != nil || len(second) != 0 {
		t.Fatalf("second Due = %+v err=%v, want no duplicate claim", second, err)
	}

	if err := store.A2APushOutbox.MarkAttempt(ctx, "push-1", time.Now().Add(-time.Second), false, "503"); err != nil {
		t.Fatal(err)
	}
	if status := pushStatus(t, store, "push-1"); status != "pending" {
		t.Fatalf("status after retry = %q", status)
	}
	again, err := store.A2APushOutbox.Due(ctx, 10, time.Now())
	if err != nil || len(again) != 1 {
		t.Fatalf("retry Due = %+v err=%v", again, err)
	}
	if err := store.A2APushOutbox.MarkDelivered(ctx, "push-1"); err != nil {
		t.Fatal(err)
	}
	if status := pushStatus(t, store, "push-1"); status != "delivered" {
		t.Fatalf("status after delivery = %q", status)
	}
}

func TestA2APushOutboxRecoversStaleSendingButStopsExhausted(t *testing.T) {
	store := a2aTaskTestStore(t, "push-stale")
	ctx := context.Background()
	seedPushTask(t, store, "tenant-push-stale", "task-1")
	if err := store.A2APushOutbox.EnqueueForTask(ctx, federation.A2APushDelivery{ID: "push-1", TaskID: "task-1", CallbackURL: "http://callback.test/hook", Payload: []byte(`{}`), MaxAttempts: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.A2APushOutbox.Due(ctx, 10, time.Now()); err != nil {
		t.Fatal(err)
	}
	oldClaim := time.Now().Add(-10 * time.Minute).UTC().Format("2006-01-02 15:04:05")
	if _, err := store.db.ExecContext(ctx, `UPDATE a2a_push_outbox SET updated_at=$2 WHERE id=$1`, "push-1", oldClaim); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.A2APushOutbox.Due(ctx, 10, time.Now())
	if err != nil || len(recovered) != 1 {
		t.Fatalf("stale Due = %+v err=%v", recovered, err)
	}
	if err := store.A2APushOutbox.MarkAttempt(ctx, "push-1", time.Now(), true, "exhausted"); err != nil {
		t.Fatal(err)
	}
	if status := pushStatus(t, store, "push-1"); status != "failed" {
		t.Fatalf("status after exhausted = %q", status)
	}
	due, err := store.A2APushOutbox.Due(ctx, 10, time.Now().Add(time.Hour))
	if err != nil || len(due) != 0 {
		t.Fatalf("exhausted Due = %+v err=%v", due, err)
	}
}

func TestA2APushOutboxListFailuresSanitized(t *testing.T) {
	store := a2aTaskTestStore(t, "push-failures")
	ctx := context.Background()
	tenant := "tenant-push-failures"
	seedPushTask(t, store, tenant, "task-1")
	seedPushTask(t, store, tenant, "task-2")
	seedPushTask(t, store, tenant, "task-3")
	for _, delivery := range []federation.A2APushDelivery{
		{ID: "f1", TaskID: "task-1", CallbackURL: "sealed:url-1", BearerToken: "sealed:token", Payload: []byte("sealed:payload"), MaxAttempts: 3},
		{ID: "f2", TaskID: "task-2", CallbackURL: "sealed:url-2", BearerToken: "sealed:token", Payload: []byte("sealed:payload"), MaxAttempts: 3},
		{ID: "d1", TaskID: "task-3", CallbackURL: "sealed:url-3", BearerToken: "sealed:token", Payload: []byte("sealed:payload"), MaxAttempts: 3},
	} {
		if err := store.A2APushOutbox.EnqueueForTask(ctx, delivery); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE a2a_push_outbox SET status='failed', last_error='callback status 500', attempts=3 WHERE id IN ('f1','f2')`); err != nil {
		t.Fatal(err)
	}

	failures, err := store.A2APushOutbox.ListFailures(ctx, tenant, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 2 {
		t.Fatalf("failures = %+v", failures)
	}
	for _, failure := range failures {
		if failure.TaskID != "task-1" && failure.TaskID != "task-2" {
			t.Fatalf("unexpected failure task = %+v", failure)
		}
		if failure.ID == "" || failure.MaxAttempts != 3 || failure.Attempts != 3 || failure.LastError == "" || failure.NextAttemptAt.IsZero() {
			t.Fatalf("failure row incomplete = %+v", failure)
		}
	}
	// The query selects only sanitized columns; callback URLs, bearer tokens,
	// and payload bodies are never part of the returned row type.
	if _, err := store.A2APushOutbox.ListFailures(ctx, "missing-tenant", 10); err != nil {
		t.Fatalf("foreign tenant listing should be empty, got err %v", err)
	}
	limited, err := store.A2APushOutbox.ListFailures(ctx, tenant, 1)
	if err != nil || len(limited) != 1 {
		t.Fatalf("limited failures = %+v err=%v", limited, err)
	}
}

func TestA2APushOutboxListDeliveriesAcrossStatuses(t *testing.T) {
	store := a2aTaskTestStore(t, "push-all-statuses")
	ctx := context.Background()
	tenant := "tenant-push-statuses"
	seedPushTask(t, store, tenant, "task-1")
	seedPushTask(t, store, tenant, "task-2")
	seedPushTask(t, store, tenant, "task-3")
	seedPushTask(t, store, tenant, "task-4")
	for _, delivery := range []federation.A2APushDelivery{
		{ID: "p1", TaskID: "task-1", CallbackURL: "sealed:url", BearerToken: "sealed:token", Payload: []byte("sealed:payload"), MaxAttempts: 3},
		{ID: "s1", TaskID: "task-2", CallbackURL: "sealed:url", BearerToken: "sealed:token", Payload: []byte("sealed:payload"), MaxAttempts: 3},
		{ID: "d1", TaskID: "task-3", CallbackURL: "sealed:url", BearerToken: "sealed:token", Payload: []byte("sealed:payload"), MaxAttempts: 3},
		{ID: "f1", TaskID: "task-4", CallbackURL: "sealed:url", BearerToken: "sealed:token", Payload: []byte("sealed:payload"), MaxAttempts: 3},
	} {
		if err := store.A2APushOutbox.EnqueueForTask(ctx, delivery); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE a2a_push_outbox SET status='sending', last_error='', attempts=1 WHERE id='s1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE a2a_push_outbox SET status='delivered', last_error='', attempts=1 WHERE id='d1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE a2a_push_outbox SET status='failed', last_error='callback status 503', attempts=3 WHERE id='f1'`); err != nil {
		t.Fatal(err)
	}

	all, err := store.A2APushOutbox.ListDeliveries(ctx, tenant, "", 50)
	if err != nil || len(all) != 4 {
		t.Fatalf("all deliveries = %d err=%v", len(all), err)
	}
	statuses := map[string]bool{}
	for _, row := range all {
		statuses[row.Status] = true
	}
	for _, want := range []string{"pending", "sending", "delivered", "failed"} {
		if !statuses[want] {
			t.Fatalf("missing %s in drill-down statuses: %v", want, statuses)
		}
	}
	delivered, err := store.A2APushOutbox.ListDeliveries(ctx, tenant, "delivered", 50)
	if err != nil || len(delivered) != 1 || delivered[0].ID != "d1" || delivered[0].Status != "delivered" {
		t.Fatalf("delivered filter = %+v err=%v", delivered, err)
	}
	for _, row := range all {
		if row.ID == "" || row.TaskID == "" || row.MaxAttempts == 0 || row.NextAttemptAt.IsZero() || row.UpdatedAt.IsZero() {
			t.Fatalf("incomplete sanitized row = %+v", row)
		}
	}
}

func TestA2APushOutboxSummary(t *testing.T) {
	store := a2aTaskTestStore(t, "push-summary")
	ctx := context.Background()
	tenant := "tenant-push-summary"
	seedPushTask(t, store, tenant, "task-1")
	seedPushTask(t, store, tenant, "task-2")
	seedPushTask(t, store, tenant, "task-3")
	if err := store.A2APushOutbox.EnqueueForTask(ctx, federation.A2APushDelivery{ID: "pending", TaskID: "task-1", CallbackURL: "sealed:url", Payload: []byte("sealed:payload"), MaxAttempts: 3}); err != nil {
		t.Fatal(err)
	}
	if err := store.A2APushOutbox.EnqueueForTask(ctx, federation.A2APushDelivery{ID: "sending", TaskID: "task-2", CallbackURL: "sealed:url", Payload: []byte("sealed:payload"), MaxAttempts: 3}); err != nil {
		t.Fatal(err)
	}
	if err := store.A2APushOutbox.EnqueueForTask(ctx, federation.A2APushDelivery{ID: "failed", TaskID: "task-3", CallbackURL: "sealed:url", Payload: []byte("sealed:payload"), MaxAttempts: 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE a2a_push_outbox SET status='sending' WHERE id='sending'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE a2a_push_outbox SET status='failed' WHERE id='failed'`); err != nil {
		t.Fatal(err)
	}
	summary, err := store.A2APushOutbox.Summary(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Pending != 1 || summary.Sending != 1 || summary.Delivered != 0 || summary.Failed != 1 || summary.EarliestNextAttemptAt == nil {
		t.Fatalf("summary = %+v", summary)
	}
}
