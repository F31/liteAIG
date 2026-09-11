package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/platform/storage/sqlite"
	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/internal/platform/webkit"
	"github.com/F31/liteAIG/migrations"
)

func TestWriteLedgerMetricsAggregatesTokenSamples(t *testing.T) {
	var body strings.Builder
	writeLedgerMetrics(func(format string, args ...any) {
		fmt.Fprintf(&body, format, args...)
	}, &sqlrepo.MetricSnapshot{
		TotalRequests: 3,
		ByOutcome: []sqlrepo.OutcomeMetric{
			{Outcome: "success", Requests: 2, InputTokens: 10, OutputTokens: 4},
			{Outcome: "error", Requests: 1, InputTokens: 3, OutputTokens: 1},
		},
	})

	metrics := body.String()
	for _, sample := range []string{
		`liteaig_request_tokens_total{kind="input"} 13`,
		`liteaig_request_tokens_total{kind="output"} 5`,
	} {
		if count := strings.Count(metrics, sample); count != 1 {
			t.Fatalf("sample %q occurred %d times in:\n%s", sample, count, metrics)
		}
	}
}

func scrapeMetrics(t *testing.T, store *sqlrepo.Store) *httptest.ResponseRecorder {
	t.Helper()
	engine := webkit.New()
	engine.Handle("GET /metrics", liteMetrics(store))
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestLiteMetricsEndpointExposition(t *testing.T) {
	db, err := sqlite.Open(context.Background(), "file:metrics-"+strings.ReplaceAll(t.Name(), "/", "-")+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	store := sqlrepo.Open(db, sqlrepo.Deps{})

	tenantID := "00000000-0000-0000-0000-000000000021"
	projectID := "00000000-0000-0000-0000-000000000022"
	requestID := "00000000-0000-0000-0000-000000000023"
	for _, stmt := range []string{
		`INSERT INTO tenants(id, public_ref, name, settlement_currency) VALUES ('` + tenantID + `', 't', 't', 'USD')`,
		`INSERT INTO projects(id, tenant_id, name) VALUES ('` + projectID + `', '` + tenantID + `', 'p')`,
		`INSERT INTO request_records(request_id, tenant_id, project_id, logical_model, outcome, input_tokens, output_tokens, latency_ms, tenant_snapshot_version, security_epoch, received_at, completed_at)
		 VALUES ('` + requestID + `', '` + tenantID + `', '` + projectID + `', 'test-model', 'success', 10, 4, 5, 1, 0, $1, $1)`,
		`INSERT INTO a2a_tasks(task_id, tenant_id, project_id, request_id, idempotency_key, status) VALUES ('task-pending', '` + tenantID + `', '` + projectID + `', 'task-pending', 'key-pending', 'completed')`,
		`INSERT INTO a2a_tasks(task_id, tenant_id, project_id, request_id, idempotency_key, status) VALUES ('task-sending', '` + tenantID + `', '` + projectID + `', 'task-sending', 'key-sending', 'completed')`,
		`INSERT INTO a2a_tasks(task_id, tenant_id, project_id, request_id, idempotency_key, status) VALUES ('task-delivered', '` + tenantID + `', '` + projectID + `', 'task-delivered', 'key-delivered', 'completed')`,
		`INSERT INTO a2a_tasks(task_id, tenant_id, project_id, request_id, idempotency_key, status) VALUES ('task-failed', '` + tenantID + `', '` + projectID + `', 'task-failed', 'key-failed', 'completed')`,
	} {
		if _, err := db.ExecContext(context.Background(), stmt, time.Now().UTC()); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	nextAttempt := time.Unix(1893456000, 0).UTC()
	for _, stmt := range []string{
		`INSERT INTO a2a_push_outbox(id, tenant_id, task_id, callback_url, payload, status, next_attempt_at) VALUES ('push-pending', '` + tenantID + `', 'task-pending', 'sealed:url', 'sealed:payload', 'pending', $1)`,
		`INSERT INTO a2a_push_outbox(id, tenant_id, task_id, callback_url, payload, status, next_attempt_at) VALUES ('push-sending', '` + tenantID + `', 'task-sending', 'sealed:url', 'sealed:payload', 'sending', $1)`,
		`INSERT INTO a2a_push_outbox(id, tenant_id, task_id, callback_url, payload, status, next_attempt_at) VALUES ('push-delivered', '` + tenantID + `', 'task-delivered', 'sealed:url', 'sealed:payload', 'delivered', $1)`,
		`INSERT INTO a2a_push_outbox(id, tenant_id, task_id, callback_url, payload, status, next_attempt_at) VALUES ('push-failed', '` + tenantID + `', 'task-failed', 'sealed:url', 'sealed:payload', 'failed', $1)`,
	} {
		if _, err := db.ExecContext(context.Background(), stmt, nextAttempt); err != nil {
			t.Fatalf("seed outbox: %v", err)
		}
	}
	for _, stmt := range []string{
		`INSERT INTO domain_event_outbox(id, kind, status, occurred_at, next_attempt_at) VALUES ('event-queued', 'system_config.update', 'queued', $1, $1)`,
		`INSERT INTO domain_event_outbox(id, kind, status, occurred_at, next_attempt_at) VALUES ('event-claiming', 'file.access', 'claiming', $1, $1)`,
		`INSERT INTO domain_event_outbox(id, kind, status, occurred_at, next_attempt_at) VALUES ('event-sent', 'file.access', 'sent', $1, $1)`,
		`INSERT INTO domain_event_outbox(id, kind, status, occurred_at, next_attempt_at) VALUES ('event-failed', 'file.access', 'failed', $1, $1)`,
	} {
		if _, err := db.ExecContext(context.Background(), stmt, nextAttempt); err != nil {
			t.Fatalf("seed event outbox: %v", err)
		}
	}

	rec := scrapeMetrics(t, store)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Fatalf("content-type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("cache-control = %q", cc)
	}
	body := rec.Body.String()
	for _, sample := range []string{
		"liteaig_up 1\n",
		"liteaig_scrape_error 0\n",
		"liteaig_requests_total 1\n",
		`liteaig_request_tokens_total{kind="input"} 10`,
		`liteaig_request_tokens_total{kind="output"} 4`,
		`liteaig_request_outcome_total{outcome="success"} 1`,
		`liteaig_request_latency_ms_total{outcome="success"} 5`,
		`liteaig_model_requests_total{model="test-model",outcome="success"} 1`,
		`liteaig_a2a_push_outbox_deliveries{status="pending"} 1`,
		`liteaig_a2a_push_outbox_deliveries{status="sending"} 1`,
		`liteaig_a2a_push_outbox_deliveries{status="delivered"} 1`,
		`liteaig_a2a_push_outbox_deliveries{status="failed"} 1`,
		`liteaig_a2a_push_outbox_next_attempt_timestamp_seconds 1893456000`,
		`liteaig_event_outbox_events{status="queued"} 1`,
		`liteaig_event_outbox_events{status="claiming"} 1`,
		`liteaig_event_outbox_events{status="sent"} 1`,
		`liteaig_event_outbox_events{status="failed"} 1`,
		`liteaig_event_outbox_next_attempt_timestamp_seconds 1893456000`,
	} {
		if count := strings.Count(body, sample); count != 1 {
			t.Errorf("sample %q occurred %d times in:\n%s", sample, count, body)
		}
	}
	for _, forbidden := range []string{requestID, tenantID, "task-pending", "push-pending", "sealed:url", "sealed:payload", "test-model-prompt", "sk-secret"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("metrics leaked %q", forbidden)
		}
	}
}

func TestLiteMetricsEndpointExposesAuditRetentionPurge(t *testing.T) {
	db, err := sqlite.Open(context.Background(), "file:metrics-retention-"+strings.ReplaceAll(t.Name(), "/", "-")+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	store := sqlrepo.Open(db, sqlrepo.Deps{})
	recordAuditPurge(7, time.Unix(1700000000, 0))
	rec := scrapeMetrics(t, store)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, sample := range []string{
		"liteaig_audit_events_purged_total 7\n",
		"liteaig_audit_purge_last_timestamp_seconds 1700000000\n",
	} {
		if count := strings.Count(body, sample); count != 1 {
			t.Errorf("sample %q occurred %d times in:\n%s", sample, count, body)
		}
	}
	recordAuditPurge(3, time.Unix(1700000060, 0))
	if auditRetentionPurge.total.Load() != 10 {
		t.Fatalf("purge counter = %d", auditRetentionPurge.total.Load())
	}
	auditRetentionPurge.total.Store(0)
	auditRetentionPurge.lastAt.Store(0)
}

func TestLiteMetricsEndpointGracefulScrapeError(t *testing.T) {
	db, err := sqlite.Open(context.Background(), "file:metrics-err-"+strings.ReplaceAll(t.Name(), "/", "-")+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	store := sqlrepo.Open(db, sqlrepo.Deps{})
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	rec := scrapeMetrics(t, store)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Fatalf("content-type = %q", ct)
	}
	if count := strings.Count(rec.Body.String(), "liteaig_scrape_error 1\n"); count != 1 {
		t.Fatalf("expected scrape_error 1, body:\n%s", rec.Body.String())
	}
}
