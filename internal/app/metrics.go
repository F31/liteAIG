package app

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/internal/platform/webkit"
)

// processStartedAt anchors the uptime gauge on the metrics endpoint.
var processStartedAt = time.Now()

// auditRetentionPurge tracks the last committed audit-retention sweep for the
// /metrics exposition. It is process-local and reset on restart.
var auditRetentionPurge = struct {
	total  atomic.Int64
	lastAt atomic.Int64 // unix seconds; 0 until the first sweep completes
}{}

var fileMappingRetentionPurge = struct {
	total  atomic.Int64
	lastAt atomic.Int64
}{}

// recordAuditPurge records one retention sweep outcome. removed may be zero;
// lastAt is the sweep time used for the freshness gauge.
func recordAuditPurge(removed int64, at time.Time) {
	auditRetentionPurge.total.Add(removed)
	auditRetentionPurge.lastAt.Store(at.Unix())
}

func recordFileMappingPurge(removed int64, at time.Time) {
	fileMappingRetentionPurge.total.Add(removed)
	fileMappingRetentionPurge.lastAt.Store(at.Unix())
}

// liteMetrics serves a dependency-free Prometheus text exposition on the Lite
// admin origin: process health, Go runtime basics, and cross-tenant aggregates
// over the durable request ledger. No prompts, keys, or per-request detail are
// ever exposed.
func liteMetrics(store *sqlrepo.Store) webkit.Handler {
	return func(c *webkit.Context) error {
		ctx := c.Request().Context()
		var body strings.Builder
		write := func(format string, args ...any) {
			fmt.Fprintf(&body, format, args...)
		}

		write("# HELP liteaig_up Whether the Lite process is up.\n")
		write("# TYPE liteaig_up gauge\n")
		write("liteaig_up 1\n")
		write("# HELP liteaig_process_uptime_seconds Process uptime.\n")
		write("# TYPE liteaig_process_uptime_seconds gauge\n")
		write("liteaig_process_uptime_seconds %g\n", time.Since(processStartedAt).Seconds())
		write("# HELP liteaig_goroutines Current number of goroutines.\n")
		write("# TYPE liteaig_goroutines gauge\n")
		write("liteaig_goroutines %d\n", runtime.NumGoroutine())

		snapshot, err := store.Accounting.Metrics(ctx)
		if err != nil {
			write("# HELP liteaig_scrape_error Whether the last metrics scrape failed.\n")
			write("# TYPE liteaig_scrape_error gauge\n")
			write("liteaig_scrape_error 1\n")
			c.Header().Set("Content-Type", "text/plain; version=0.0.4")
			_, _ = c.Response().Write([]byte(body.String()))
			return nil
		}
		write("liteaig_scrape_error 0\n")
		write("# HELP liteaig_requests_total Total requests recorded in the ledger.\n")
		write("# TYPE liteaig_requests_total counter\n")
		writeLedgerMetrics(write, snapshot)
		writeA2APushOutboxMetrics(ctx, write, store)
		writeEventOutboxMetrics(ctx, write, store)
		writeAuditRetentionMetrics(write)
		writeFileMappingRetentionMetrics(write)

		c.Header().Set("Content-Type", "text/plain; version=0.0.4")
		c.Header().Set("Cache-Control", "no-store")
		_, err = c.Response().Write([]byte(body.String()))
		return err
	}
}

func writeA2APushOutboxMetrics(ctx context.Context, write func(string, ...any), store *sqlrepo.Store) {
	if store == nil || store.A2APushOutbox == nil {
		return
	}
	summary, err := store.A2APushOutbox.SummaryAll(ctx)
	if err != nil {
		write("# HELP liteaig_a2a_push_outbox_scrape_error Whether the last A2A push outbox metrics scrape failed.\n")
		write("# TYPE liteaig_a2a_push_outbox_scrape_error gauge\n")
		write("liteaig_a2a_push_outbox_scrape_error 1\n")
		return
	}
	write("# HELP liteaig_a2a_push_outbox_deliveries Current durable A2A push callbacks by status.\n")
	write("# TYPE liteaig_a2a_push_outbox_deliveries gauge\n")
	write("liteaig_a2a_push_outbox_deliveries{status=\"pending\"} %d\n", summary.Pending)
	write("liteaig_a2a_push_outbox_deliveries{status=\"sending\"} %d\n", summary.Sending)
	write("liteaig_a2a_push_outbox_deliveries{status=\"delivered\"} %d\n", summary.Delivered)
	write("liteaig_a2a_push_outbox_deliveries{status=\"failed\"} %d\n", summary.Failed)
	if summary.EarliestNextAttemptAt != nil {
		write("# HELP liteaig_a2a_push_outbox_next_attempt_timestamp_seconds Earliest pending A2A push retry timestamp.\n")
		write("# TYPE liteaig_a2a_push_outbox_next_attempt_timestamp_seconds gauge\n")
		write("liteaig_a2a_push_outbox_next_attempt_timestamp_seconds %d\n", summary.EarliestNextAttemptAt.Unix())
	}
}

func writeEventOutboxMetrics(ctx context.Context, write func(string, ...any), store *sqlrepo.Store) {
	if store == nil || store.EventOutbox == nil {
		return
	}
	summary, err := store.EventOutbox.SummaryAll(ctx)
	if err != nil {
		write("# HELP liteaig_event_outbox_scrape_error Whether the last event outbox metrics scrape failed.\n")
		write("# TYPE liteaig_event_outbox_scrape_error gauge\n")
		write("liteaig_event_outbox_scrape_error 1\n")
		return
	}
	write("# HELP liteaig_event_outbox_events Current durable domain events by status.\n")
	write("# TYPE liteaig_event_outbox_events gauge\n")
	write("liteaig_event_outbox_events{status=\"queued\"} %d\n", summary.Queued)
	write("liteaig_event_outbox_events{status=\"claiming\"} %d\n", summary.Claiming)
	write("liteaig_event_outbox_events{status=\"sent\"} %d\n", summary.Sent)
	write("liteaig_event_outbox_events{status=\"failed\"} %d\n", summary.Failed)
	if summary.EarliestNextAttemptAt != nil {
		write("# HELP liteaig_event_outbox_next_attempt_timestamp_seconds Earliest queued domain event retry timestamp.\n")
		write("# TYPE liteaig_event_outbox_next_attempt_timestamp_seconds gauge\n")
		write("liteaig_event_outbox_next_attempt_timestamp_seconds %d\n", summary.EarliestNextAttemptAt.Unix())
	}
}

func writeAuditRetentionMetrics(write func(string, ...any)) {
	write("# HELP liteaig_audit_events_purged_total Cumulative audit events removed by retention sweeps.\n")
	write("# TYPE liteaig_audit_events_purged_total counter\n")
	write("liteaig_audit_events_purged_total %d\n", auditRetentionPurge.total.Load())
	if last := auditRetentionPurge.lastAt.Load(); last > 0 {
		write("# HELP liteaig_audit_purge_last_timestamp_seconds Last audit retention sweep completion (unix seconds).\n")
		write("# TYPE liteaig_audit_purge_last_timestamp_seconds gauge\n")
		write("liteaig_audit_purge_last_timestamp_seconds %d\n", last)
	}
}

func writeFileMappingRetentionMetrics(write func(string, ...any)) {
	write("# HELP liteaig_file_mappings_purged_total Cumulative local file mappings removed by retention sweeps.\n")
	write("# TYPE liteaig_file_mappings_purged_total counter\n")
	write("liteaig_file_mappings_purged_total %d\n", fileMappingRetentionPurge.total.Load())
	if last := fileMappingRetentionPurge.lastAt.Load(); last > 0 {
		write("# HELP liteaig_file_mapping_purge_last_timestamp_seconds Last file mapping retention sweep completion (unix seconds).\n")
		write("# TYPE liteaig_file_mapping_purge_last_timestamp_seconds gauge\n")
		write("liteaig_file_mapping_purge_last_timestamp_seconds %d\n", last)
	}
}

func writeLedgerMetrics(write func(string, ...any), snapshot *sqlrepo.MetricSnapshot) {
	write("liteaig_requests_total %d\n", snapshot.TotalRequests)
	write("# HELP liteaig_request_outcome_total Requests by outcome.\n")
	write("# TYPE liteaig_request_outcome_total counter\n")
	var inputTokens, outputTokens int64
	for _, metric := range snapshot.ByOutcome {
		write("liteaig_request_outcome_total{outcome=%q} %d\n", metric.Outcome, metric.Requests)
		write("liteaig_request_latency_ms_total{outcome=%q} %d\n", metric.Outcome, metric.LatencyMS)
		inputTokens += metric.InputTokens
		outputTokens += metric.OutputTokens
	}
	write("# HELP liteaig_request_tokens_total Tokens consumed by direction.\n")
	write("# TYPE liteaig_request_tokens_total counter\n")
	write("liteaig_request_tokens_total{kind=\"input\"} %d\n", inputTokens)
	write("liteaig_request_tokens_total{kind=\"output\"} %d\n", outputTokens)
	write("# HELP liteaig_model_requests_total Requests by logical model and outcome.\n")
	write("# TYPE liteaig_model_requests_total counter\n")
	for _, metric := range snapshot.ByModel {
		write("liteaig_model_requests_total{model=%q,outcome=%q} %d\n", metric.LogicalModel, metric.Outcome, metric.Requests)
		write("liteaig_model_latency_ms_total{model=%q,outcome=%q} %d\n", metric.LogicalModel, metric.Outcome, metric.LatencyMS)
	}
}
