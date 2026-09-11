package config

import (
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/runtime"
)

func snapshot(version, epoch int64, publishedAt time.Time) *runtime.TenantRuntimeSnapshot {
	return runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Status: "active",
		Version: version, SecurityEpoch: epoch, PublishedAt: publishedAt,
	})
}

func TestDriftNoneWhenMatching(t *testing.T) {
	now := time.Unix(100, 0)
	report := DetectDrift(snapshot(3, 1, now.Add(-time.Minute)), 3, runtime.DriftMetadata{GraceMS: 5 * 60_000, Strict: true, SecurityEpoch: 1}, now)
	if report.Level != DriftNone || !report.IsReady() {
		t.Fatalf("report = %+v", report)
	}
}

func TestDriftBeyondGraceIsCriticalAndNotReady(t *testing.T) {
	now := time.Unix(100, 0)
	report := DetectDrift(snapshot(2, 1, now.Add(-10*time.Minute)), 3, runtime.DriftMetadata{GraceMS: 5 * 60_000}, now)
	if report.Level != DriftCritical || report.IsReady() {
		t.Fatalf("report = %+v", report)
	}
}

func TestDriftWithinGraceIsWarnAndReady(t *testing.T) {
	now := time.Unix(100, 0)
	report := DetectDrift(snapshot(2, 1, now.Add(-time.Minute)), 3, runtime.DriftMetadata{GraceMS: 5 * 60_000}, now)
	if report.Level != DriftWarn || !report.IsReady() {
		t.Fatalf("report = %+v", report)
	}
}

func TestNoSnapshotIsCriticalAndNotReady(t *testing.T) {
	report := DetectDrift(nil, 3, runtime.DriftMetadata{}, time.Unix(100, 0))
	if report.Level != DriftCritical || report.IsReady() {
		t.Fatalf("report = %+v", report)
	}
}

func TestStrictStaleSecurityEpochIsCriticalAndNotReady(t *testing.T) {
	now := time.Unix(100, 0)
	report := DetectDrift(snapshot(3, 1, now.Add(-time.Minute)), 3, runtime.DriftMetadata{GraceMS: 60_000, Strict: true, SecurityEpoch: 2}, now)
	if report.Level != DriftCritical || report.IsReady() {
		t.Fatalf("report = %+v", report)
	}
}

func TestReporterReceivesDrift(t *testing.T) {
	var got []DriftLevel
	now := time.Unix(100, 0)
	DetectAndReport(snapshot(2, 1, now.Add(-time.Minute)), 3, runtime.DriftMetadata{GraceMS: 60_000}, now, func(report DriftReport) {
		got = append(got, report.Level)
	})
	if len(got) != 1 || got[0] != DriftWarn {
		t.Fatalf("reporter = %v", got)
	}
}
