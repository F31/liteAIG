package observability

import (
	"context"
	"errors"
	"testing"

	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRequestSpanCorrelatesRequestIDAndSnapshotVersion(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := NewTracer(provider)
	defer func() { _ = provider.Shutdown(context.Background()) }()

	request := &kernel.RequestContext{
		RequestID:   "req-1",
		Snapshot:    runtime.NewTenantSnapshot(runtime.TenantSnapshotData{TenantID: "t", TenantRef: "ref", Version: 7}),
		Interaction: &interaction.Context{TenantID: "t", ProjectID: "p", SessionID: "session", TaskID: "task", RootTaskID: "root", ParentTaskID: "parent"},
	}
	ctx, span := tracer.Start(context.Background(), request, "admission")
	span.End(ctx, nil)

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d", len(spans))
	}
	attrs := spans[0].Attributes
	foundRequestID := false
	foundVersion := false
	foundTask := false
	for _, attr := range attrs {
		switch string(attr.Key) {
		case "liteaig.request_id":
			if attr.Value.AsString() != "req-1" {
				t.Fatalf("request_id = %v", attr.Value.AsString())
			}
			foundRequestID = true
		case "liteaig.snapshot_version":
			if attr.Value.AsInt64() != 7 {
				t.Fatalf("snapshot_version = %v", attr.Value.AsInt64())
			}
			foundVersion = true
		case "liteaig.root_task_id":
			foundTask = attr.Value.AsString() == "root"
		}
	}
	if !foundRequestID || !foundVersion || !foundTask {
		t.Fatal("span missing correlation attributes")
	}
}

func TestSpanRecordsErrorStatus(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := NewTracer(provider)
	defer func() { _ = provider.Shutdown(context.Background()) }()

	ctx, span := tracer.Start(context.Background(), nil, "execution")
	span.End(ctx, errors.New("upstream down"))
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Status.Code != codes.Error {
		t.Fatalf("spans = %+v", spans)
	}
}

func TestLowCardinalityValidation(t *testing.T) {
	sink := NewMetricSink(nil)
	if err := sink.Record(Metric{Name: "liteaig.requests", Value: 1, Labels: map[string]string{"provider": "openai"}}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Record(Metric{Name: "bad", Value: 1, Labels: map[string]string{"request_id": "req-1"}}); err == nil {
		t.Fatal("high-cardinality label accepted")
	}
	if err := sink.Record(Metric{Labels: map[string]string{}}); err == nil {
		t.Fatal("empty metric name accepted")
	}
}

func TestLatencyBreakdownSeparatesPhases(t *testing.T) {
	breakdown := LatencyBreakdown{CoreMS: 5, ProviderNetworkMS: 40, ExternalGuardrailMS: 15}
	if breakdown.Total() != 60 {
		t.Fatalf("total = %d", breakdown.Total())
	}
	var merged LatencyBreakdown
	merged.Add(breakdown)
	merged.Add(LatencyBreakdown{CoreMS: 1, ProviderNetworkMS: 2, ExternalGuardrailMS: 3})
	if merged.CoreMS != 6 || merged.ProviderNetworkMS != 42 || merged.ExternalGuardrailMS != 18 {
		t.Fatalf("merged = %+v", merged)
	}
}
