package observability

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/F31/liteAIG/internal/platform/egress"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otellog "go.opentelemetry.io/otel/log"
	metricapi "go.opentelemetry.io/otel/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const otlpTimeout = 3 * time.Second

const traceTimeout = otlpTimeout

// NewOTLPTracer enables OTLP/HTTP protobuf at an explicit URL. Empty disables
// tracing without creating a provider, worker or network client.
func NewOTLPTracer(ctx context.Context, endpoint string) (*Tracer, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, nil
	}
	u, client, err := otlpClient(endpoint, "/v1/traces")
	if err != nil {
		return nil, err
	}
	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(u.String()),
		otlptracehttp.WithHTTPClient(client),
		otlptracehttp.WithHeaders(map[string]string{}),
		otlptracehttp.WithCompression(otlptracehttp.NoCompression),
		otlptracehttp.WithTimeout(traceTimeout),
		otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}),
	)
	if err != nil {
		client.CloseIdleConnections()
		return nil, errors.New("OTLP exporter initialization failed")
	}
	// The SDK's WithResource merges resource.Environment(), which imports
	// OTEL_RESOURCE_ATTRIBUTES / OTEL_SERVICE_NAME from the ambient process.
	// This exporter is explicitly pinned (endpoint, headers, resource), so
	// ambient OTel resource attributes must not be able to inject arbitrary
	// data into the trace stream. Suppress them for the duration of provider
	// construction, which is the only point they are consulted.
	restore := suppressOTELResourceEnv()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "liteaig"))),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
		sdktrace.WithBatcher(exporter,
			sdktrace.WithMaxQueueSize(2048), sdktrace.WithMaxExportBatchSize(512),
			sdktrace.WithBatchTimeout(time.Second), sdktrace.WithExportTimeout(traceTimeout)),
	)
	restore()
	tracer := NewTracer(provider)
	tracer.closeClient = client.CloseIdleConnections
	return tracer, nil
}

// OTLPMetrics exports MetricSink records over OTLP/HTTP protobuf. Empty
// endpoint disables export without creating a worker or network client.
type OTLPMetrics struct {
	provider    *sdkmetric.MeterProvider
	meter       metricapi.Meter
	mu          sync.Mutex
	counters    map[string]metricapi.Float64Counter
	closeClient func()
}

func NewOTLPMetrics(ctx context.Context, endpoint string) (*OTLPMetrics, error) {
	u, client, err := otlpClient(endpoint, "/v1/metrics")
	if err != nil || u == nil {
		return nil, err
	}
	exporter, err := otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithEndpointURL(u.String()),
		otlpmetrichttp.WithHTTPClient(client),
		otlpmetrichttp.WithHeaders(map[string]string{}),
		otlpmetrichttp.WithCompression(otlpmetrichttp.NoCompression),
		otlpmetrichttp.WithTimeout(otlpTimeout),
		otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{Enabled: false}),
	)
	if err != nil {
		client.CloseIdleConnections()
		return nil, errors.New("OTLP metrics exporter initialization failed")
	}
	restore := suppressOTELResourceEnv()
	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(resource.NewSchemaless(attribute.String("service.name", "liteaig"))),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(time.Second), sdkmetric.WithTimeout(otlpTimeout))),
	)
	restore()
	return &OTLPMetrics{provider: provider, meter: provider.Meter("liteaig.gateway"), counters: map[string]metricapi.Float64Counter{}, closeClient: client.CloseIdleConnections}, nil
}

func (m *OTLPMetrics) Record(ctx context.Context, metric Metric) error {
	if m == nil {
		return nil
	}
	if metric.Value < 0 {
		return errors.New("metric value must be non-negative")
	}
	if err := ValidateLowCardinality(metric.Labels); err != nil {
		return err
	}
	counter, err := m.counter(metric.Name)
	if err != nil {
		return err
	}
	counter.Add(ctx, metric.Value, metricapi.WithAttributes(metricAttributes(metric.Labels)...))
	return nil
}

func (m *OTLPMetrics) counter(name string) (metricapi.Float64Counter, error) {
	if name == "" {
		return nil, errors.New("metric name is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if counter, ok := m.counters[name]; ok {
		return counter, nil
	}
	counter, err := m.meter.Float64Counter(name)
	if err != nil {
		return nil, err
	}
	m.counters[name] = counter
	return counter, nil
}

func (m *OTLPMetrics) Shutdown(ctx context.Context) error {
	if m == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, otlpTimeout)
	defer cancel()
	err := m.provider.Shutdown(ctx)
	if m.closeClient != nil {
		m.closeClient()
	}
	if err != nil {
		return errors.New("OTLP metrics shutdown failed")
	}
	return nil
}

// LogRecord is a sanitized operational log record for OTLP export. Callers own
// the message and attributes; ambient OTel headers/resources are never imported.
type LogRecord struct {
	Message    string
	Severity   string
	Attributes map[string]string
	Timestamp  time.Time
}

type OTLPLogger struct {
	provider    *sdklog.LoggerProvider
	logger      otellog.Logger
	closeClient func()
}

func NewOTLPLogger(ctx context.Context, endpoint string) (*OTLPLogger, error) {
	u, client, err := otlpClient(endpoint, "/v1/logs")
	if err != nil || u == nil {
		return nil, err
	}
	exporter, err := otlploghttp.New(ctx,
		otlploghttp.WithEndpointURL(u.String()),
		otlploghttp.WithHTTPClient(client),
		otlploghttp.WithHeaders(map[string]string{}),
		otlploghttp.WithCompression(otlploghttp.NoCompression),
		otlploghttp.WithTimeout(otlpTimeout),
		otlploghttp.WithRetry(otlploghttp.RetryConfig{Enabled: false}),
	)
	if err != nil {
		client.CloseIdleConnections()
		return nil, errors.New("OTLP logs exporter initialization failed")
	}
	restore := suppressOTELResourceEnv()
	provider := sdklog.NewLoggerProvider(
		sdklog.WithResource(resource.NewSchemaless(attribute.String("service.name", "liteaig"))),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter, sdklog.WithMaxQueueSize(2048), sdklog.WithExportMaxBatchSize(512), sdklog.WithExportInterval(time.Second), sdklog.WithExportTimeout(otlpTimeout))),
	)
	restore()
	return &OTLPLogger{provider: provider, logger: provider.Logger("liteaig.gateway"), closeClient: client.CloseIdleConnections}, nil
}

func (l *OTLPLogger) Emit(ctx context.Context, value LogRecord) error {
	if l == nil {
		return nil
	}
	if value.Message == "" {
		return errors.New("log message is required")
	}
	var record otellog.Record
	stamp := value.Timestamp
	if stamp.IsZero() {
		stamp = time.Now()
	}
	record.SetTimestamp(stamp)
	record.SetObservedTimestamp(stamp)
	record.SetBody(otellog.StringValue(value.Message))
	record.SetSeverity(logSeverity(value.Severity))
	record.SetSeverityText(strings.ToUpper(value.Severity))
	record.AddAttributes(logAttributes(value.Attributes)...)
	l.logger.Emit(ctx, record)
	return nil
}

func (l *OTLPLogger) Shutdown(ctx context.Context) error {
	if l == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, otlpTimeout)
	defer cancel()
	err := l.provider.Shutdown(ctx)
	if l.closeClient != nil {
		l.closeClient()
	}
	if err != nil {
		return errors.New("OTLP logs shutdown failed")
	}
	return nil
}

func otlpClient(endpoint, defaultPath string) (*url.URL, *http.Client, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, nil, nil
	}
	u, err := url.Parse(endpoint)
	policy := egress.LitePolicy()
	policy.DisableProxy = true // A proxy must not bypass destination DNS/IP checks.
	policy.MaxResponseBytes = 64 << 10
	if err != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || egress.ValidateTarget(endpoint, policy) != nil {
		return nil, nil, errors.New("invalid or denied OTLP endpoint")
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = defaultPath
	}
	client := egress.Client(policy)
	client.Timeout = otlpTimeout
	return u, client, nil
}

func metricAttributes(labels map[string]string) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(labels))
	for key, value := range labels {
		attrs = append(attrs, attribute.String(key, value))
	}
	return attrs
}

func logAttributes(labels map[string]string) []otellog.KeyValue {
	attrs := make([]otellog.KeyValue, 0, len(labels))
	for key, value := range labels {
		attrs = append(attrs, otellog.String(key, value))
	}
	return attrs
}

func logSeverity(value string) otellog.Severity {
	switch strings.ToLower(value) {
	case "debug":
		return otellog.SeverityDebug
	case "warn", "warning":
		return otellog.SeverityWarn
	case "error":
		return otellog.SeverityError
	case "fatal":
		return otellog.SeverityFatal
	default:
		return otellog.SeverityInfo
	}
}

func suppressOTELResourceEnv() func() {
	keys := []string{"OTEL_RESOURCE_ATTRIBUTES", "OTEL_SERVICE_NAME"}
	saved := make([]string, len(keys))
	had := make([]bool, len(keys))
	for i, key := range keys {
		saved[i], had[i] = os.LookupEnv(key)
		_ = os.Unsetenv(key)
	}
	return func() {
		for i, key := range keys {
			if had[i] {
				_ = os.Setenv(key, saved[i])
			} else {
				_ = os.Unsetenv(key)
			}
		}
	}
}

// Shutdown flushes queued spans and stops the worker within three seconds (or
// the caller's earlier deadline). SDK shutdown is idempotent. Nil is disabled.
func (t *Tracer) Shutdown(ctx context.Context) error {
	if t == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, traceTimeout)
	defer cancel()
	err := t.provider.Shutdown(ctx)
	if t.closeClient != nil {
		t.closeClient()
	}
	if err != nil {
		return errors.New("OTLP shutdown failed")
	}
	return nil
}
