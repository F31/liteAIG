package benchmark

import "time"

// EvidenceRecord is one read-only export line. Sensitive content (prompt,
// response, secret) is never included by construction.
type EvidenceRecord struct {
	Kind          string
	TenantID      string
	ResourceID    string
	Version       int64
	SecurityEpoch int64
	OccurredAt    time.Time
	Detail        map[string]string
}

// Exporter assembles a read-only evidence archive for a tenant/time range.
// It excludes prompt/response bodies and secrets by default.
type Exporter struct {
	now func() time.Time
}

// NewExporter builds an evidence exporter.
func NewExporter(now func() time.Time) *Exporter {
	if now == nil {
		now = time.Now
	}
	return &Exporter{now: now}
}

// Archive is the exported evidence bundle.
type Archive struct {
	TenantID   string
	From, To   time.Time
	ExportedAt time.Time
	Records    []EvidenceRecord
}

// Export assembles the supplied records into a read-only archive. Records are
// copied; a nil Detail is normalized to an empty map.
func (e *Exporter) Export(tenantID string, from, to time.Time, records []EvidenceRecord) Archive {
	out := make([]EvidenceRecord, 0, len(records))
	for _, record := range records {
		if record.Detail == nil {
			record.Detail = map[string]string{}
		}
		out = append(out, record)
	}
	return Archive{TenantID: tenantID, From: from, To: to, ExportedAt: e.now().UTC(), Records: out}
}
