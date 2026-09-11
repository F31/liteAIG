package contracts

// LiveEvent is a summary-only per-request telemetry fact emitted by the Data
// Plane for operational observers (live tail). It never carries request
// bodies or content.
type LiveEvent struct {
	RequestID    string
	Outcome      string
	DeploymentID string
	LatencyMS    int64
}

// TelemetrySink receives live request summaries from the Data Plane without
// exposing an observability implementation.
type TelemetrySink interface {
	PublishLive(LiveEvent)
}
