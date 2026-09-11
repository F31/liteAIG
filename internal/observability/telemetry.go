// Package observability exposes OTel-backed tracing and low-cardinality metrics
// behind the Kernel event contract. Business modules emit standard events; this
// package is where the exporter is wired.
package observability

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/F31/liteAIG/internal/kernel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// RequestSpan is a start handle for a request lifecycle span.
type RequestSpan interface {
	End(context.Context, error)
}

// Tracer starts request lifecycle spans correlated with Request ID and snapshot.
type Tracer struct {
	provider    *sdktrace.TracerProvider
	tracer      trace.Tracer
	closeClient func()
}

// NewTracer builds a tracer over an OTel tracer provider. The provider may use
// the real exporter in production or an in-memory exporter in tests.
func NewTracer(provider *sdktrace.TracerProvider) *Tracer {
	return &Tracer{provider: provider, tracer: provider.Tracer("liteaig.gateway")}
}

// TracerProvider exposes the underlying provider for shutdown/export in tests.
func (t *Tracer) TracerProvider() *sdktrace.TracerProvider { return t.provider }

// Start begins a request span with low-cardinality attributes.
func (t *Tracer) Start(ctx context.Context, request *kernel.RequestContext, stage string) (context.Context, RequestSpan) {
	if request == nil {
		spanCtx, span := t.tracer.Start(ctx, stage)
		return spanCtx, otelSpan{span: span}
	}
	attrs := []attribute.KeyValue{
		attribute.String("liteaig.request_id", request.RequestID),
		attribute.Int64("liteaig.snapshot_version", request.SnapshotVersion()),
		attribute.String("liteaig.tenant_id", request.TenantID()),
		attribute.String("liteaig.project_id", request.ProjectID()),
	}
	if request.Interaction != nil {
		attrs = append(attrs,
			attribute.String("liteaig.session_id", request.Interaction.SessionID),
			attribute.String("liteaig.task_id", request.Interaction.TaskID),
			attribute.String("liteaig.root_task_id", request.Interaction.RootTaskID),
			attribute.String("liteaig.parent_task_id", request.Interaction.ParentTaskID),
		)
	}
	spanCtx, span := t.tracer.Start(ctx, stage, trace.WithAttributes(attrs...))
	return spanCtx, otelSpan{span: span}
}

type otelSpan struct{ span trace.Span }

func (s otelSpan) End(ctx context.Context, err error) {
	// The status code records the outcome, but the message is deliberately left
	// empty: error text may embed prompt/response fragments or secret values and
	// must never reach the OTLP destination.
	if err != nil {
		s.span.SetStatus(codes.Error, "")
	} else {
		s.span.SetStatus(codes.Ok, "")
	}
	s.span.End()
}

// ValidateLowCardinality rejects metrics labels that would explode cardinality.
func ValidateLowCardinality(labels map[string]string) error {
	highCardinality := map[string]bool{
		"request_id": true, "session_id": true, "api_key_id": true,
		"user_id": true, "trace_id": true, "span_id": true,
	}
	for key := range labels {
		if highCardinality[key] {
			return fmt.Errorf("metric label %q is high cardinality", key)
		}
	}
	return nil
}

// Metric is a low-cardinality measurement.
type Metric struct {
	Name   string
	Value  float64
	Labels map[string]string
}

// MetricSink records low-cardinality metrics.
type MetricSink struct {
	recorder func(Metric)
}

// NewMetricSink builds a sink that forwards to a recorder (e.g., OTel meter).
func NewMetricSink(recorder func(Metric)) *MetricSink {
	return &MetricSink{recorder: recorder}
}

// Record validates cardinality before recording.
func (s *MetricSink) Record(metric Metric) error {
	if metric.Name == "" {
		return errors.New("metric name is required")
	}
	if err := ValidateLowCardinality(metric.Labels); err != nil {
		return err
	}
	if s.recorder != nil {
		s.recorder(metric)
	}
	return nil
}

// LatencyBreakdown separates Core, provider-network, and external-guardrail time.
type LatencyBreakdown struct {
	CoreMS              int64
	ProviderNetworkMS   int64
	ExternalGuardrailMS int64
}

// Add merges another breakdown.
func (b *LatencyBreakdown) Add(other LatencyBreakdown) {
	b.CoreMS += other.CoreMS
	b.ProviderNetworkMS += other.ProviderNetworkMS
	b.ExternalGuardrailMS += other.ExternalGuardrailMS
}

// Total returns the sum of all phases.
func (b LatencyBreakdown) Total() int64 {
	return b.CoreMS + b.ProviderNetworkMS + b.ExternalGuardrailMS
}

// SortMetricLabels returns labels sorted by key for stable output.
func SortMetricLabels(labels map[string]string) map[string]string {
	if len(labels) < 2 {
		return labels
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return labels
}
