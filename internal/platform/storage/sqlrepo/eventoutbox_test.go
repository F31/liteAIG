package sqlrepo

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/platform/storage/sqlite"
	"github.com/F31/liteAIG/migrations"
)

func TestDomainEventOutboxEnqueue(t *testing.T) {
	db, err := sqlite.Open(context.Background(), "file:event-outbox?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	outbox := NewDomainEventOutbox(db)
	event := contracts.DomainEvent{ID: "e0000000-0000-4000-8000-000000000001", Kind: "system_config.update", TenantID: "tenant", ProjectID: "project", RequestID: "request", OccurredAt: time.Unix(100, 0), Attributes: map[string]string{"severity": "medium"}}
	if err := outbox.Enqueue(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := outbox.Enqueue(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	var count int
	var kind, status, attributes string
	var tenantID sql.NullString
	if err := db.QueryRow(`SELECT COUNT(*), MAX(kind), MAX(status), MAX(attributes), MAX(tenant_id) FROM domain_event_outbox`).Scan(&count, &kind, &status, &attributes, &tenantID); err != nil {
		t.Fatal(err)
	}
	if count != 1 || kind != "system_config.update" || status != "queued" || attributes != `{"severity":"medium"}` || !tenantID.Valid || tenantID.String != "tenant" {
		t.Fatalf("outbox row count=%d kind=%s status=%s attrs=%s tenant=%v", count, kind, status, attributes, tenantID)
	}
	if err := outbox.Enqueue(context.Background(), contracts.DomainEvent{}); err != nil {
		t.Fatal(err)
	}
}

func TestDomainEventOutboxClaimDeliverLifecycle(t *testing.T) {
	db, err := sqlite.Open(context.Background(), "file:event-outbox-lifecycle?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	outbox := NewDomainEventOutbox(db)
	now := time.Unix(200, 0).UTC()
	event := contracts.DomainEvent{ID: "e0000000-0000-4000-8000-000000000002", Kind: "system_config.update", TenantID: "tenant", OccurredAt: now, Attributes: map[string]string{"action": "update"}}
	if err := outbox.Enqueue(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `UPDATE domain_event_outbox SET next_attempt_at=$1, status='queued'`, time.Unix(100, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	due, err := outbox.Due(context.Background(), 10, time.Unix(150, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != event.ID || due[0].Kind != "system_config.update" || due[0].Attributes["action"] != "update" || due[0].TenantID != "tenant" {
		t.Fatalf("due = %+v", due)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM domain_event_outbox WHERE id=$1`, event.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "claiming" {
		t.Fatalf("status after claim = %s", status)
	}
	if err := outbox.MarkDelivered(context.Background(), event.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM domain_event_outbox WHERE id=$1`, event.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "sent" {
		t.Fatalf("status after deliver = %s", status)
	}
}

func TestDomainEventOutboxRetryExhaustion(t *testing.T) {
	db, err := sqlite.Open(context.Background(), "file:event-outbox-retry?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	outbox := NewDomainEventOutbox(db)
	now := time.Unix(200, 0).UTC()
	event := contracts.DomainEvent{ID: "e0000000-0000-4000-8000-000000000003", Kind: "file.access", OccurredAt: now}
	if err := outbox.Enqueue(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `UPDATE domain_event_outbox SET next_attempt_at=$1`, time.Unix(100, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	if err := outbox.MarkFailed(context.Background(), event.ID, time.Unix(300, 0).UTC(), true); err != nil {
		t.Fatal(err)
	}
	var status string
	var attempts int
	if err := db.QueryRow(`SELECT status, attempts FROM domain_event_outbox WHERE id=$1`, event.ID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || attempts != 1 {
		t.Fatalf("status=%s attempts=%d", status, attempts)
	}
	// A failed event is still reclaimable via Due.
	if _, err := db.ExecContext(context.Background(), `UPDATE domain_event_outbox SET next_attempt_at=$1`, time.Unix(100, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	due, err := outbox.Due(context.Background(), 10, time.Unix(150, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 {
		t.Fatalf("failed event not reclaimable: %+v", due)
	}
}

func TestDomainEventOutboxSummaryAll(t *testing.T) {
	db, err := sqlite.Open(context.Background(), "file:event-outbox-summary?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	outbox := NewDomainEventOutbox(db)
	now := time.Unix(200, 0).UTC()
	for i := 0; i < 2; i++ {
		if err := outbox.Enqueue(context.Background(), contracts.DomainEvent{ID: fmt.Sprintf("e0000000-0000-4000-8000-00000000000%d", i+4), Kind: "file.access", OccurredAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(context.Background(), `UPDATE domain_event_outbox SET status='sent' WHERE id=$1`, "e0000000-0000-4000-8000-000000000004"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `UPDATE domain_event_outbox SET status='failed' WHERE id=$1`, "e0000000-0000-4000-8000-000000000005"); err != nil {
		t.Fatal(err)
	}
	summary, err := outbox.SummaryAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Queued != 0 || summary.Claiming != 0 || summary.Sent != 1 || summary.Failed != 1 {
		t.Fatalf("summary = %+v", summary)
	}
}
