package app

import (
	"testing"
	"time"
)

func TestRoutingMetricsSnapshotAndHealth(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := fixedClock{now: now}
	metrics := newRoutingMetrics(clock)

	metrics.begin("dep-a")
	metrics.begin("dep-a")
	metrics.observe("dep-a", 120, 0.01, 0, true)
	metrics.observe("dep-a", 80, 0.01, 0, true)

	snapshot := metrics.snapshot(now)
	depA, ok := snapshot["dep-a"]
	if !ok {
		t.Fatal("dep-a missing from snapshot")
	}
	// First observation seeds the EMA at 120, then the second blends 0.3*80 +
	// 0.7*120 = 108. Cost stays 0.01.
	if depA.LatencyMS != 108 || depA.Cost < 0.0099 || depA.Cost > 0.0101 {
		t.Fatalf("dep-a latency=%v cost=%v", depA.LatencyMS, depA.Cost)
	}
	if depA.Load != 0 {
		t.Fatalf("healthy dep-a load=%v want 0", depA.Load)
	}
	if !metrics.healthy("dep-a", now) {
		t.Fatal("healthy dep-a reported unhealthy")
	}
}

func TestRoutingMetricsFailureRaisesLoadAndHealth(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := fixedClock{now: now}
	metrics := newRoutingMetrics(clock)

	for i := 0; i < 4; i++ {
		metrics.begin("dep-b")
		metrics.observe("dep-b", 50, 0.02, 0, i%2 == 0) // 2 success, 2 failed
	}
	if !metrics.healthy("dep-b", now) {
		t.Fatal("50% failure must stay healthy (below 50% threshold)")
	}
	for i := 0; i < 4; i++ {
		metrics.begin("dep-b")
		metrics.observe("dep-b", 50, 0.02, 0, false) // push past 50% failures
	}
	if metrics.healthy("dep-b", now) {
		t.Fatal("majority-failure dep-b must be unhealthy")
	}
	snapshot := metrics.snapshot(now)
	if snapshot["dep-b"].Load <= 0 {
		t.Fatalf("failure dep-b load=%v want >0", snapshot["dep-b"].Load)
	}
}

func TestRoutingMetricsStaleWindowDropped(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := fixedClock{now: now}
	metrics := newRoutingMetrics(clock)
	metrics.begin("dep-c")
	metrics.observe("dep-c", 10, 0.001, 0, true)

	if _, ok := metrics.snapshot(now)["dep-c"]; !ok {
		t.Fatal("fresh dep-c missing")
	}
	stale := now.Add(metricsWindow + time.Second)
	if _, ok := metrics.snapshot(stale)["dep-c"]; ok {
		t.Fatal("stale dep-c must be dropped from snapshot")
	}
	if !metrics.healthy("dep-c", stale) {
		t.Fatal("stale (unknown) dep-c must be healthy")
	}
}

func TestRoutingMetricsCacheAffinityEWMA(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := fixedClock{now: now}
	metrics := newRoutingMetrics(clock)

	// First request: 90 of 100 input tokens came from the provider cache.
	metrics.begin("dep-cache")
	metrics.observe("dep-cache", 50, 0.01, 0.9, true)
	// Second request: full cache hit (ratio 1.0): EWMA = 0.3*1.0 + 0.7*0.9.
	metrics.begin("dep-cache")
	metrics.observe("dep-cache", 50, 0.01, 1.0, true)

	snapshot := metrics.snapshot(now)
	affinity := snapshot["dep-cache"].CacheAffinity
	// seed 0.9 then blend 0.3*1.0 + 0.7*0.9 = 0.93
	if affinity < 0.92 || affinity > 0.94 {
		t.Fatalf("cache affinity EWMA = %v, want ~0.93", affinity)
	}

	// A deployment with no cache hits reports affinity 0 (not NaN/negative).
	metrics.begin("dep-nocache")
	metrics.observe("dep-nocache", 50, 0.01, 0, true)
	if affinity := metrics.snapshot(now)["dep-nocache"].CacheAffinity; affinity != 0 {
		t.Fatalf("no-cache affinity = %v, want 0", affinity)
	}
}

func TestRoutingMetricsUnknownHealthy(t *testing.T) {
	metrics := newRoutingMetrics(fixedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)})
	if !metrics.healthy("never-seen", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatal("never-observed deployment must be healthy")
	}
}
