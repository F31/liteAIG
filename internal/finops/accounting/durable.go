package accounting

import (
	"context"
	"errors"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/tenancy"
)

// SpoolMetrics records low-cardinality spool telemetry.
type SpoolMetrics interface {
	RecordSpoolUsage(events, bytes int64, oldest time.Duration)
	RecordSpoolIngested(count int64)
}

// FuncSpoolMetrics adapts callbacks into SpoolMetrics without importing an
// observability implementation, so the accounting domain stays dependency-free.
type FuncSpoolMetrics struct {
	Usage    func(events, bytes int64, oldest time.Duration)
	Ingested func(count int64)
}

func (f FuncSpoolMetrics) RecordSpoolUsage(events, bytes int64, oldest time.Duration) {
	if f.Usage != nil {
		f.Usage(events, bytes, oldest)
	}
}

func (f FuncSpoolMetrics) RecordSpoolIngested(count int64) {
	if f.Ingested != nil {
		f.Ingested(count)
	}
}

// SpoolRepository implements Repository by durably appending to a Spool.
// Authoritative reads are served by the ingest repository after replay, so
// read methods return not-found on the spool path.
type SpoolRepository struct {
	spool  Spool
	policy SpoolPolicy
}

// NewSpoolRepository builds a Repository that persists finalized facts to the spool.
func NewSpoolRepository(spool Spool, policy SpoolPolicy) *SpoolRepository {
	return &SpoolRepository{spool: spool, policy: policy}
}

// Finalize appends the facts to the spool after applying the per-Tenant quota
// and hard-mode pressure gates.
func (r *SpoolRepository) Finalize(ctx context.Context, facts Facts) (bool, error) {
	if r.policy.QuotaBytes > 0 {
		_, bytes, err := r.spool.TenantUsage(ctx, facts.TenantID)
		if err != nil {
			return false, ErrSpoolUnavailable
		}
		if bytes+estimateSize(facts) > r.policy.QuotaBytes {
			return false, ErrSpoolFull
		}
	}
	if r.policy.Mode == "hard" {
		stats, err := r.spool.Stats(ctx)
		if err != nil {
			return false, ErrSpoolUnavailable
		}
		if ClassifySpoolPressure(ratioFor(r.policy, stats.Events, stats.Bytes), r.policy) == SpoolHard {
			return false, ErrSpoolFull
		}
	}
	record := SpoolRecord{
		EventID:   facts.UsageEventID,
		RequestID: facts.RequestID,
		TenantID:  facts.TenantID,
		ProjectID: facts.ProjectID,
		EventTS:   facts.CompletedAt,
		Facts:     facts,
	}
	if err := r.spool.Append(ctx, record); err != nil {
		return false, err
	}
	return true, nil
}

func (r *SpoolRepository) GetRequest(context.Context, tenancy.TenantScope, string) (*RequestRecord, error) {
	return nil, tenancy.ErrNotFound
}
func (r *SpoolRepository) ListRequests(context.Context, tenancy.TenantScope, int) ([]RequestRecord, error) {
	return nil, tenancy.ErrNotFound
}
func (r *SpoolRepository) GetUsage(context.Context, tenancy.TenantScope, string) (*UsageRecord, error) {
	return nil, tenancy.ErrNotFound
}

var _ Repository = (*SpoolRepository)(nil)

func estimateSize(facts Facts) int64 {
	return int64(len(facts.RequestID) + len(facts.UsageEventID) + len(facts.TenantID) + len(facts.LogicalModel) + 256)
}

// FlusherConfig controls the ingest loop batch and interval.
type FlusherConfig struct {
	Batch    int
	Interval time.Duration
}

// Flusher drains uncommitted spool events into the ingest repository and
// commits acknowledged events, retrying on ingest failure.
type Flusher struct {
	spool   Spool
	ingest  Repository
	config  FlusherConfig
	metrics SpoolMetrics
	clock   contracts.Clock
	sink    contracts.EventSink
}

// NewFlusher builds a background accounting ingester.
func NewFlusher(spool Spool, ingest Repository, config FlusherConfig, metrics SpoolMetrics, clock contracts.Clock, sink contracts.EventSink) *Flusher {
	if config.Batch <= 0 {
		config.Batch = 100
	}
	if config.Interval <= 0 {
		config.Interval = time.Second
	}
	return &Flusher{spool: spool, ingest: ingest, config: config, metrics: metrics, clock: clock, sink: sink}
}

// RunOnce ingests one batch of uncommitted events and commits the acknowledged ones.
func (f *Flusher) RunOnce(ctx context.Context) (int, error) {
	pending, err := f.spool.Pending(ctx, f.config.Batch)
	if err != nil {
		return 0, err
	}
	var committed []string
	for _, record := range pending {
		if _, err := f.ingest.Finalize(ctx, record.Facts); err != nil {
			if f.sink != nil {
				_ = f.sink.Emit(ctx, contracts.DomainEvent{ID: record.EventID + ".flush_failed", Kind: "accounting.spool.flush_failed", OccurredAt: f.clock.Now(), TenantID: record.TenantID, ProjectID: record.ProjectID, RequestID: record.RequestID, Attributes: map[string]string{"event_id": record.EventID}})
			}
			break
		}
		committed = append(committed, record.EventID)
	}
	if len(committed) == 0 {
		return 0, nil
	}
	if err := f.spool.Commit(ctx, committed); err != nil {
		return len(committed), err
	}
	if f.metrics != nil {
		f.metrics.RecordSpoolIngested(int64(len(committed)))
	}
	return len(committed), nil
}

// Run loops RunOnce until the context is cancelled.
func (f *Flusher) Run(ctx context.Context) {
	ticker := time.NewTicker(f.config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = f.RunOnce(ctx)
		}
	}
}

// DurableFinalizer wraps a Finalizer whose repository is a SpoolRepository and
// applies accounting fail modes plus pressure alerting around the finalize call.
type DurableFinalizer struct {
	finalizer *Finalizer
	spool     Spool
	policy    SpoolPolicy
	metrics   SpoolMetrics
	sink      contracts.EventSink
	clock     contracts.Clock
}

// NewDurableFinalizer builds a durable finalizer.
func NewDurableFinalizer(finalizer *Finalizer, spool Spool, policy SpoolPolicy, metrics SpoolMetrics, sink contracts.EventSink, clock contracts.Clock) *DurableFinalizer {
	return &DurableFinalizer{finalizer: finalizer, spool: spool, policy: policy, metrics: metrics, sink: sink, clock: clock}
}

// Finalize finalizes the request through the spool-backed repository and applies
// soft-accounting degradation plus spool pressure alerts.
func (d *DurableFinalizer) Finalize(ctx context.Context, facts Facts) (Result, error) {
	result, err := d.finalizer.Finalize(ctx)
	d.recordPressure(ctx, facts)
	if err != nil && (errors.Is(err, ErrSpoolUnavailable) || errors.Is(err, ErrSpoolFull)) && d.policy.Mode == "soft" {
		severity := "critical"
		kind := "accounting.spool.unavailable"
		if errors.Is(err, ErrSpoolFull) {
			kind = "accounting.spool.full"
		}
		_ = d.sink.Emit(ctx, contracts.DomainEvent{ID: facts.UsageEventID + ".spool", Kind: kind, OccurredAt: d.clock.Now(), TenantID: facts.TenantID, ProjectID: facts.ProjectID, RequestID: facts.RequestID, Attributes: map[string]string{"mode": "soft", "severity": severity}})
		return result, nil
	}
	return result, err
}

func (d *DurableFinalizer) recordPressure(ctx context.Context, facts Facts) {
	stats, err := d.spool.Stats(ctx)
	if err != nil {
		return
	}
	if d.metrics != nil {
		d.metrics.RecordSpoolUsage(stats.Events, stats.Bytes, stats.OldestAge)
	}
	pressure := ClassifySpoolPressure(ratioFor(d.policy, stats.Events, stats.Bytes), d.policy)
	if pressure == SpoolWarn || pressure == SpoolCritical {
		kind := "accounting.spool.warning"
		severity := "warning"
		if pressure == SpoolCritical {
			kind = "accounting.spool.critical"
			severity = "critical"
		}
		if d.sink != nil {
			_ = d.sink.Emit(ctx, contracts.DomainEvent{ID: facts.UsageEventID + ".pressure", Kind: kind, OccurredAt: d.clock.Now(), TenantID: facts.TenantID, ProjectID: facts.ProjectID, RequestID: facts.RequestID, Attributes: map[string]string{"severity": severity, "mode": d.policy.Mode}})
		}
	}
}
