package observability

import (
	"time"

	"github.com/F31/liteAIG/internal/cache"
	"github.com/F31/liteAIG/internal/finops/accounting"
)

// CacheMetricsAdapter forwards Exact Cache statistics to the observability
// MetricSink with low-cardinality labels.
type CacheMetricsAdapter struct{ sink *MetricSink }

func NewCacheMetricsAdapter(sink *MetricSink) *CacheMetricsAdapter {
	return &CacheMetricsAdapter{sink: sink}
}

func (a *CacheMetricsAdapter) Record(stats cache.Stats) {
	if a.sink == nil {
		return
	}
	_ = a.sink.Record(Metric{Name: "cache.hits", Value: float64(stats.Hits)})
	_ = a.sink.Record(Metric{Name: "cache.misses", Value: float64(stats.Misses)})
	_ = a.sink.Record(Metric{Name: "cache.evictions", Value: float64(stats.Evictions)})
	_ = a.sink.Record(Metric{Name: "cache.savings_tokens", Value: float64(stats.SavingsTokens)})
}

var _ cache.Metrics = (*CacheMetricsAdapter)(nil)

// SpoolMetricsAdapter forwards accounting spool telemetry to the MetricSink.
type SpoolMetricsAdapter struct{ sink *MetricSink }

func NewSpoolMetricsAdapter(sink *MetricSink) *SpoolMetricsAdapter {
	return &SpoolMetricsAdapter{sink: sink}
}

func (a *SpoolMetricsAdapter) RecordSpoolUsage(events, bytes int64, oldest time.Duration) {
	if a.sink == nil {
		return
	}
	_ = a.sink.Record(Metric{Name: "accounting_spool_events", Value: float64(events)})
	_ = a.sink.Record(Metric{Name: "accounting_spool_bytes", Value: float64(bytes)})
	_ = a.sink.Record(Metric{Name: "accounting_spool_oldest_age_seconds", Value: oldest.Seconds()})
}

func (a *SpoolMetricsAdapter) RecordSpoolIngested(count int64) {
	if a.sink == nil {
		return
	}
	_ = a.sink.Record(Metric{Name: "accounting_spool_ingested", Value: float64(count)})
}

var _ accounting.SpoolMetrics = (*SpoolMetricsAdapter)(nil)
