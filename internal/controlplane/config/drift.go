package config

import (
	"time"

	"github.com/F31/liteAIG/internal/kernel/runtime"
)

// DriftLevel expresses how far live runtime diverges from the published version.
type DriftLevel string

const (
	DriftNone     DriftLevel = "none"
	DriftWarn     DriftLevel = "warn"
	DriftCritical DriftLevel = "critical"
)

// DriftReport describes live-vs-published divergence and readiness.
type DriftReport struct {
	Level            DriftLevel
	LiveVersion      int64
	PublishedVersion int64
	VersionAgeMS     int64
	GraceMS          int64
	BeyondGrace      bool
	EpochStale       bool
	Strict           bool
	HasSnapshot      bool
	Reasons          []string
}

// DetectDrift compares a live snapshot against the published version metadata.
func DetectDrift(snapshot *runtime.TenantRuntimeSnapshot, publishedVersion int64, drift runtime.DriftMetadata, now time.Time) DriftReport {
	report := DriftReport{
		LiveVersion:      liveVersion(snapshot),
		PublishedVersion: publishedVersion,
		GraceMS:          drift.GraceMS,
		Strict:           drift.Strict,
		HasSnapshot:      snapshot != nil,
	}
	if snapshot == nil {
		report.Level = DriftCritical
		report.Reasons = append(report.Reasons, "no_active_snapshot")
		return report
	}
	if snapshot.Version != publishedVersion {
		report.Reasons = append(report.Reasons, "version_mismatch")
	}
	report.VersionAgeMS = now.Sub(snapshot.PublishedAt).Milliseconds()
	if drift.GraceMS > 0 && report.VersionAgeMS > drift.GraceMS {
		report.BeyondGrace = true
		report.Reasons = append(report.Reasons, "beyond_grace")
	}
	if drift.Strict && snapshot.SecurityEpoch < drift.SecurityEpoch {
		report.EpochStale = true
		report.Reasons = append(report.Reasons, "security_epoch_stale")
	}

	switch {
	case report.EpochStale:
		report.Level = DriftCritical
	case report.BeyondGrace:
		report.Level = DriftCritical
	case len(report.Reasons) > 0:
		report.Level = DriftWarn
	default:
		report.Level = DriftNone
	}
	return report
}

func liveVersion(snapshot *runtime.TenantRuntimeSnapshot) int64 {
	if snapshot == nil {
		return 0
	}
	return snapshot.Version
}

// IsReady reports whether the gateway is ready to serve based on drift.
func (r DriftReport) IsReady() bool {
	if !r.HasSnapshot {
		return false
	}
	if r.BeyondGrace {
		return false
	}
	if r.Strict && r.EpochStale {
		return false
	}
	return r.Level == DriftNone || r.Level == DriftWarn
}

// Reporter receives drift events for metrics/alerts without coupling to a sink.
type Reporter func(DriftReport)

// DetectAndReport combines detection with an optional reporter callback.
func DetectAndReport(snapshot *runtime.TenantRuntimeSnapshot, publishedVersion int64, drift runtime.DriftMetadata, now time.Time, report Reporter) DriftReport {
	result := DetectDrift(snapshot, publishedVersion, drift, now)
	if report != nil {
		report(result)
	}
	return result
}
