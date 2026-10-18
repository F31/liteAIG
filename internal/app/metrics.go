package app

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/internal/platform/webkit"
)

// processStartedAt anchors the uptime gauge on the metrics endpoint.
var processStartedAt = time.Now()

// processMetricKey identifies one provider/outcome series. Provider IDs are
// bounded by the configured catalog, so this stays low-cardinality.
type processMetricKey struct{ provider, outcome string }

type processHistogram struct {
	buckets []float64
	counts  []uint64
	sum     float64
	count   uint64
}

func newProcessHistogram(buckets []float64) *processHistogram {
	return &processHistogram{buckets: append([]float64(nil), buckets...), counts: make([]uint64, len(buckets))}
}

func (h *processHistogram) observe(value float64) {
	if value < 0 {
		value = 0
	}
	h.sum += value
	h.count++
	for index, upper := range h.buckets {
		if value <= upper {
			h.counts[index]++
		}
	}
}

var processMetrics = struct {
	sync.Mutex
	requests map[processMetricKey]uint64
	latency  map[processMetricKey]*processHistogram
	ttft     map[processMetricKey]*processHistogram
}{
	requests: map[processMetricKey]uint64{},
	latency:  map[processMetricKey]*processHistogram{},
	ttft:     map[processMetricKey]*processHistogram{},
}

var latencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
var ttftBuckets = []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}

// recordProcessMetrics collects provider-level latency and TTFT histograms for
// the Prometheus endpoint without touching the database on every scrape.
func recordProcessMetrics(providerID, outcome string, latency, ttft time.Duration) {
	if providerID == "" {
		providerID = "unknown"
	}
	if outcome == "" {
		outcome = "unknown"
	}
	key := processMetricKey{provider: providerID, outcome: outcome}
	processMetrics.Lock()
	defer processMetrics.Unlock()
	processMetrics.requests[key]++
	if processMetrics.latency[key] == nil {
		processMetrics.latency[key] = newProcessHistogram(latencyBuckets)
	}
	processMetrics.latency[key].observe(latency.Seconds())
	if ttft > 0 {
		if processMetrics.ttft[key] == nil {
			processMetrics.ttft[key] = newProcessHistogram(ttftBuckets)
		}
		processMetrics.ttft[key].observe(ttft.Seconds())
	}
}

func sortedProcessKeys(values map[processMetricKey]uint64) []processMetricKey {
	keys := make([]processMetricKey, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].provider == keys[j].provider {
			return keys[i].outcome < keys[j].outcome
		}
		return keys[i].provider < keys[j].provider
	})
	return keys
}

func writeProcessHistogramSamples(write func(string, ...any), name, labels string, histogram *processHistogram) {
	if histogram == nil {
		return
	}
	for index, upper := range histogram.buckets {
		write("%s_bucket{%s,le=%q} %d\n", name, labels, strconv.FormatFloat(upper, 'g', -1, 64), histogram.counts[index])
	}
	write("%s_bucket{%s,le=\"+Inf\"} %d\n", name, labels, histogram.count)
	write("%s_sum{%s} %g\n", name, labels, histogram.sum)
	write("%s_count{%s} %d\n", name, labels, histogram.count)
}

func writeProcessMetrics(write func(string, ...any)) {
	processMetrics.Lock()
	requests := make(map[processMetricKey]uint64, len(processMetrics.requests))
	for key, value := range processMetrics.requests {
		requests[key] = value
	}
	latency := make(map[processMetricKey]*processHistogram, len(processMetrics.latency))
	for key, value := range processMetrics.latency {
		copyValue := *value
		copyValue.counts = append([]uint64(nil), value.counts...)
		latency[key] = &copyValue
	}
	ttft := make(map[processMetricKey]*processHistogram, len(processMetrics.ttft))
	for key, value := range processMetrics.ttft {
		copyValue := *value
		copyValue.counts = append([]uint64(nil), value.counts...)
		ttft[key] = &copyValue
	}
	processMetrics.Unlock()

	keys := sortedProcessKeys(requests)
	if len(keys) > 0 {
		write("# HELP liteaig_provider_requests_total Upstream requests by provider and outcome.\n")
		write("# TYPE liteaig_provider_requests_total counter\n")
		for _, key := range keys {
			write("liteaig_provider_requests_total{provider=%q,outcome=%q} %d\n", key.provider, key.outcome, requests[key])
		}
	}
	if len(keys) > 0 {
		write("# HELP liteaig_provider_request_duration_seconds Upstream request latency by provider and outcome.\n")
		write("# TYPE liteaig_provider_request_duration_seconds histogram\n")
		for _, key := range keys {
			labels := fmt.Sprintf("provider=%q,outcome=%q", key.provider, key.outcome)
			writeProcessHistogramSamples(write, "liteaig_provider_request_duration_seconds", labels, latency[key])
		}
		if len(ttft) > 0 {
			write("# HELP liteaig_provider_ttft_seconds Time to first streamed chunk by provider and outcome.\n")
			write("# TYPE liteaig_provider_ttft_seconds histogram\n")
			for _, key := range keys {
				labels := fmt.Sprintf("provider=%q,outcome=%q", key.provider, key.outcome)
				writeProcessHistogramSamples(write, "liteaig_provider_ttft_seconds", labels, ttft[key])
			}
		}
	}
}

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
		writeProcessMetrics(write)
		writeAuxiliaryMetrics(write)
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

type auxiliaryMetricKey struct{ kind, outcome string }

var auxiliaryMetrics = struct {
	sync.Mutex
	calls  map[auxiliaryMetricKey]uint64
	tokens map[auxiliaryMetricKey]uint64
}{
	calls:  map[auxiliaryMetricKey]uint64{},
	tokens: map[auxiliaryMetricKey]uint64{},
}

// recordAuxiliaryCall makes semantic-cache embeddings and LLM-as-Judge calls
// observable even though they are outside the main request budget/accounting
// path. This is an explicit exemption, not an accounting substitute.
func recordAuxiliaryCall(kind, outcome string, tokens int64) {
	if outcome == "" {
		outcome = "unknown"
	}
	key := auxiliaryMetricKey{kind: kind, outcome: outcome}
	auxiliaryMetrics.Lock()
	auxiliaryMetrics.calls[key]++
	if tokens > 0 {
		auxiliaryMetrics.tokens[key] += uint64(tokens)
	}
	auxiliaryMetrics.Unlock()
}

func writeAuxiliaryMetrics(write func(string, ...any)) {
	auxiliaryMetrics.Lock()
	calls := make(map[auxiliaryMetricKey]uint64, len(auxiliaryMetrics.calls))
	for key, value := range auxiliaryMetrics.calls {
		calls[key] = value
	}
	tokens := make(map[auxiliaryMetricKey]uint64, len(auxiliaryMetrics.tokens))
	for key, value := range auxiliaryMetrics.tokens {
		tokens[key] = value
	}
	auxiliaryMetrics.Unlock()
	if len(calls) == 0 {
		return
	}
	write("# HELP liteaig_auxiliary_calls_total Auxiliary provider calls outside the main budget/accounting path.\n")
	write("# TYPE liteaig_auxiliary_calls_total counter\n")
	for key, value := range calls {
		write("liteaig_auxiliary_calls_total{kind=%q,outcome=%q} %d\n", key.kind, key.outcome, value)
	}
	write("# HELP liteaig_auxiliary_tokens_total Tokens consumed by auxiliary provider calls.\n")
	write("# TYPE liteaig_auxiliary_tokens_total counter\n")
	for key, value := range tokens {
		write("liteaig_auxiliary_tokens_total{kind=%q,outcome=%q} %d\n", key.kind, key.outcome, value)
	}
}
