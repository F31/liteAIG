package observability

import (
	"sync"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/cache"
)

func TestCacheMetricsAdapterForwardsToSink(t *testing.T) {
	var mu sync.Mutex
	recorded := map[string]float64{}
	sink := NewMetricSink(func(metric Metric) {
		mu.Lock()
		recorded[metric.Name] = metric.Value
		mu.Unlock()
	})
	adapter := NewCacheMetricsAdapter(sink)
	adapter.Record(cache.Stats{Hits: 3, Misses: 1, Evictions: 2, SavingsTokens: 100})
	mu.Lock()
	defer mu.Unlock()
	if recorded["cache.hits"] != 3 || recorded["cache.misses"] != 1 || recorded["cache.savings_tokens"] != 100 {
		t.Fatalf("recorded = %v", recorded)
	}
}

func TestSpoolMetricsAdapterForwardsToSink(t *testing.T) {
	var mu sync.Mutex
	recorded := map[string]float64{}
	sink := NewMetricSink(func(metric Metric) {
		mu.Lock()
		recorded[metric.Name] = metric.Value
		mu.Unlock()
	})
	adapter := NewSpoolMetricsAdapter(sink)
	adapter.RecordSpoolUsage(10, 2048, 30*time.Second)
	adapter.RecordSpoolIngested(4)
	mu.Lock()
	defer mu.Unlock()
	if recorded["accounting_spool_events"] != 10 || recorded["accounting_spool_ingested"] != 4 {
		t.Fatalf("recorded = %v", recorded)
	}
}
