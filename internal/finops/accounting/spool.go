// Package accounting owns immutable request and usage finalization facts,
// including the durable local accounting spool contract.
package accounting

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrSpoolUnavailable reports that a spool append could not be persisted.
	ErrSpoolUnavailable = errors.New("accounting spool unavailable")
	// ErrSpoolFull reports that a spool has reached its configured hard threshold.
	ErrSpoolFull = errors.New("accounting spool full")
)

// SpoolRecord is one durable accounting event persisted on the local WAL.
type SpoolRecord struct {
	EventID        string
	RequestID      string
	TenantID       string
	ProjectID      string
	EventTS        time.Time
	PricingVersion string
	Facts          Facts
	Checksum       string
}

// SpoolStats reports local spool usage for threshold evaluation.
type SpoolStats struct {
	Events     int64
	Bytes      int64
	OldestAge  time.Duration
	UsageRatio float64 // 0..1 relative to the configured per-tenant/global quota
}

// Spool is the durable local accounting recovery store.
type Spool interface {
	// Append durably records an event; returns an error when it cannot be persisted.
	Append(context.Context, SpoolRecord) error
	// Pending returns up to limit uncommitted events for ingest.
	Pending(context.Context, int) ([]SpoolRecord, error)
	// Commit marks events as ingested so their segments can be truncated.
	Commit(context.Context, []string) error
	// Stats reports spool usage for threshold and alert evaluation.
	Stats(context.Context) (SpoolStats, error)
	// TenantUsage reports events and bytes accumulated by one Tenant so that
	// per-Tenant quotas can be enforced by the accounting policy.
	TenantUsage(context.Context, string) (events, bytes int64, err error)
}

// SpoolPolicy is the per-Tenant accounting durability policy.
type SpoolPolicy struct {
	Mode              string  // hard|soft
	WarnThreshold     float64 // default 0.7
	CriticalThreshold float64 // default 0.9
	HardThreshold     float64 // default 1.0
	QuotaBytes        int64   // per-Tenant spool quota in bytes; <=0 means unbounded
}

// SpoolPressure classifies spool usage for alerting and admission.
type SpoolPressure int

const (
	SpoolOK SpoolPressure = iota
	SpoolWarn
	SpoolCritical
	SpoolHard
)

// ClassifySpoolPressure maps usage ratio to a pressure level using the policy thresholds.
func ClassifySpoolPressure(ratio float64, policy SpoolPolicy) SpoolPressure {
	switch {
	case policy.HardThreshold > 0 && ratio >= policy.HardThreshold:
		return SpoolHard
	case policy.CriticalThreshold > 0 && ratio >= policy.CriticalThreshold:
		return SpoolCritical
	case policy.WarnThreshold > 0 && ratio >= policy.WarnThreshold:
		return SpoolWarn
	default:
		return SpoolOK
	}
}

// Ratio computes the usage ratio for an event count, or for bytes when the
// policy carries a byte quota.
func ratioFor(policy SpoolPolicy, events, bytes int64) float64 {
	if policy.QuotaBytes > 0 {
		return float64(bytes) / float64(policy.QuotaBytes)
	}
	// Without a quota, scale against a large fixed event bound so pressure
	// remains meaningful for alerting. A zero quota means unbounded and the
	// ratio stays 0 unless events exceed the bound.
	const defaultEventBound = 1_000_000
	return float64(events) / float64(defaultEventBound)
}
