package observability

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"go.opentelemetry.io/otel/trace"
	logcollector "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	metriccollector "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestOTLPExportPrivacyAndCorrelation(t *testing.T) {
	// Generic OTel configuration must neither enable another destination nor
	// inject headers/resource secrets into this explicitly configured exporter.
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=secret-header")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "Authorization=secret-header")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "secret=secret-resource")
	t.Setenv("OTEL_SERVICE_NAME", "secret-service")
	for _, path := range []string{"", "/custom/traces"} {
		t.Run(path, func(t *testing.T) {
			received := make(chan *collector.ExportTraceServiceRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wantPath := path
				if wantPath == "" {
					wantPath = "/v1/traces"
				}
				if r.Method != "POST" || r.URL.Path != wantPath || r.Header.Get("Content-Type") != "application/x-protobuf" || r.Header.Get("Authorization") != "" {
					t.Errorf("unexpected OTLP request: %s %s %v", r.Method, r.URL.Path, r.Header)
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				for _, secret := range []string{"private-prompt", "private-response", "private-error", "secret-header", "secret-resource", "secret-service", "private-key"} {
					if bytes.Contains(body, []byte(secret)) {
						t.Errorf("export leaked %s", secret)
					}
				}
				var export collector.ExportTraceServiceRequest
				if err := proto.Unmarshal(body, &export); err != nil {
					t.Error(err)
				}
				received <- &export
				w.Header().Set("Content-Type", "application/x-protobuf")
			}))
			defer server.Close()
			tracer, err := NewOTLPTracer(context.Background(), server.URL+path)
			if err != nil {
				t.Fatal(err)
			}
			defer tracer.Shutdown(context.Background())
			traceID, _ := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
			parentID, _ := trace.SpanIDFromHex("0123456789abcdef")
			parent := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: parentID, TraceFlags: trace.FlagsSampled, Remote: true})
			ctx := trace.ContextWithRemoteSpanContext(context.Background(), parent)
			request := &kernel.RequestContext{
				RequestID:   "existing-request-id",
				Snapshot:    runtime.NewTenantSnapshot(runtime.TenantSnapshotData{Version: 7}),
				Interaction: &interaction.Context{TenantID: "tenant", ProjectID: "project"},
				Request:     &interaction.UnifiedRequest{Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Content: "private-prompt"}}}},
				Response:    &interaction.UnifiedResponse{Choices: []interaction.Choice{{Message: interaction.Message{Content: "private-response"}}}},
				Key:         runtime.APIKey{PublicID: "private-key"},
			}
			ctx, span := tracer.Start(ctx, request, "liteaig.pipeline")
			span.End(ctx, errors.New("private-error"))
			if err := tracer.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			select {
			case export := <-received:
				if len(export.ResourceSpans) != 1 || len(export.ResourceSpans[0].ScopeSpans) != 1 || len(export.ResourceSpans[0].ScopeSpans[0].Spans) != 1 {
					t.Fatalf("export = %v", export)
				}
				s := export.ResourceSpans[0].ScopeSpans[0].Spans[0]
				if !bytes.Equal(s.TraceId, traceID[:]) || !bytes.Equal(s.ParentSpanId, parentID[:]) || s.Name != "liteaig.pipeline" || s.EndTimeUnixNano < s.StartTimeUnixNano {
					t.Fatalf("span = %v", s)
				}
				if s.Status.Code != tracepb.Status_STATUS_CODE_ERROR || s.Status.Message != "" || len(s.Events) != 0 {
					t.Fatalf("unsafe status/events: %v", s)
				}
				found := false
				for _, attr := range s.Attributes {
					if attr.Key == "liteaig.request_id" {
						found = attr.Value.GetStringValue() == request.RequestID
					}
				}
				if !found || request.RequestID != "existing-request-id" {
					t.Fatal("request ID not preserved")
				}
			case <-time.After(time.Second):
				t.Fatal("no real export")
			}
		})
	}
}

func TestOTLPMetricsAndLogsExportPrivacy(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=secret-header")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_HEADERS", "Authorization=secret-metric-header")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_HEADERS", "Authorization=secret-log-header")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "secret=secret-resource")
	t.Setenv("OTEL_SERVICE_NAME", "secret-service")

	t.Run("metrics", func(t *testing.T) {
		received := make(chan *metriccollector.ExportMetricsServiceRequest, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" || r.URL.Path != "/v1/metrics" || r.Header.Get("Content-Type") != "application/x-protobuf" || r.Header.Get("Authorization") != "" {
				t.Errorf("unexpected OTLP metrics request: %s %s %v", r.Method, r.URL.Path, r.Header)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			for _, secret := range []string{"secret-header", "secret-metric-header", "secret-resource", "secret-service"} {
				if bytes.Contains(body, []byte(secret)) {
					t.Errorf("metrics export leaked %s", secret)
				}
			}
			var export metriccollector.ExportMetricsServiceRequest
			if err := proto.Unmarshal(body, &export); err != nil {
				t.Error(err)
			}
			received <- &export
			w.Header().Set("Content-Type", "application/x-protobuf")
		}))
		defer server.Close()
		metrics, err := NewOTLPMetrics(context.Background(), server.URL)
		if err != nil {
			t.Fatal(err)
		}
		if err := metrics.Record(context.Background(), Metric{Name: "liteaig_test_requests", Value: 1, Labels: map[string]string{"outcome": "success"}}); err != nil {
			t.Fatal(err)
		}
		if err := metrics.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		select {
		case export := <-received:
			if len(export.ResourceMetrics) == 0 {
				t.Fatalf("empty metrics export: %v", export)
			}
		case <-time.After(time.Second):
			t.Fatal("no metrics export")
		}
	})

	t.Run("logs", func(t *testing.T) {
		received := make(chan *logcollector.ExportLogsServiceRequest, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" || r.URL.Path != "/v1/logs" || r.Header.Get("Content-Type") != "application/x-protobuf" || r.Header.Get("Authorization") != "" {
				t.Errorf("unexpected OTLP logs request: %s %s %v", r.Method, r.URL.Path, r.Header)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			for _, secret := range []string{"private-body", "secret-header", "secret-log-header", "secret-resource", "secret-service"} {
				if bytes.Contains(body, []byte(secret)) {
					t.Errorf("logs export leaked %s", secret)
				}
			}
			var export logcollector.ExportLogsServiceRequest
			if err := proto.Unmarshal(body, &export); err != nil {
				t.Error(err)
			}
			received <- &export
			w.Header().Set("Content-Type", "application/x-protobuf")
		}))
		defer server.Close()
		logger, err := NewOTLPLogger(context.Background(), server.URL)
		if err != nil {
			t.Fatal(err)
		}
		if err := logger.Emit(context.Background(), LogRecord{Message: "request completed", Severity: "info", Attributes: map[string]string{"component": "gateway"}}); err != nil {
			t.Fatal(err)
		}
		if err := logger.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		select {
		case export := <-received:
			if len(export.ResourceLogs) == 0 {
				t.Fatalf("empty logs export: %v", export)
			}
		case <-time.After(time.Second):
			t.Fatal("no logs export")
		}
	})
}

func TestOTLPDisabledAndInvalid(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", server.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", server.URL)
	tracer, err := NewOTLPTracer(context.Background(), " ")
	if err != nil || tracer != nil {
		t.Fatalf("disabled = %v, %v", tracer, err)
	}
	metrics, err := NewOTLPMetrics(context.Background(), " ")
	if err != nil || metrics != nil {
		t.Fatalf("disabled metrics = %v, %v", metrics, err)
	}
	logger, err := NewOTLPLogger(context.Background(), " ")
	if err != nil || logger != nil {
		t.Fatalf("disabled logger = %v, %v", logger, err)
	}
	if err := tracer.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"localhost:4318", "grpc://localhost:4317", "http://169.254.169.254", "http://10.0.0.1", "http://[fd00::1]", "http://user:secret@localhost", "http://localhost?secret=key", "http://localhost/#secret", "://"} {
		if tracer, err := NewOTLPTracer(context.Background(), endpoint); tracer != nil || err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("endpoint validation: %v, %v", tracer, err)
		}
		if metrics, err := NewOTLPMetrics(context.Background(), endpoint); metrics != nil || err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("metrics endpoint validation: %v, %v", metrics, err)
		}
		if logger, err := NewOTLPLogger(context.Background(), endpoint); logger != nil || err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("logs endpoint validation: %v, %v", logger, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("disabled exporter made network requests")
	}
}

func TestOTLPFailuresAndBoundedShutdown(t *testing.T) {
	for _, mode := range []string{"unavailable", "redirect", "stalled"} {
		t.Run(mode, func(t *testing.T) {
			var redirected atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
			defer target.Close()
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "unavailable":
					w.WriteHeader(http.StatusServiceUnavailable)
				case "redirect":
					http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
				case "stalled":
					<-release
				}
			}))
			defer server.Close()
			defer unblock()
			tracer, err := NewOTLPTracer(context.Background(), server.URL)
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			for i := 0; i < 4096; i++ {
				ctx, span := tracer.Start(context.Background(), nil, "liteaig.pipeline")
				span.End(ctx, nil)
			}
			if time.Since(start) > time.Second {
				t.Fatal("export blocked request path")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			start = time.Now()
			_ = tracer.Shutdown(ctx)
			if time.Since(start) > time.Second {
				t.Fatal("shutdown ignored deadline")
			}
			unblock()
			if redirected.Load() != 0 {
				t.Fatal("export followed redirect")
			}
		})
	}
}
