// Package sink provides a replaceable, buffered, idempotent analytics sink
// (ClickHouse-compatible HTTP backend) that forwards redacted Domain Events off
// the request hot path.
package sink

import (
	"context"
	"sync"
	"time"
)

// AnalyticsEvent is one redacted analytics payload with a stable id.
type AnalyticsEvent struct {
	ID         string
	Kind       string
	TenantID   string
	ProjectID  string
	RequestID  string
	OccurredAt time.Time
	Attributes map[string]string
}

// Writer flushes a batch of analytics events to the backend.
type Writer interface {
	WriteBatch(context.Context, []AnalyticsEvent) error
}

// AnalyticsSink buffers and flushes analytics events.
type AnalyticsSink interface {
	Write(context.Context, AnalyticsEvent)
	Flush(context.Context) error
}

// Config controls buffering.
type Config struct {
	BatchSize     int
	FlushInterval time.Duration
	BacklogLimit  int
	MaxBackoff    time.Duration
}

func (c Config) withDefaults() Config {
	if c.BatchSize <= 0 {
		c.BatchSize = 100
	}
	if c.FlushInterval <= 0 {
		c.FlushInterval = time.Second
	}
	if c.BacklogLimit <= 0 {
		c.BacklogLimit = 10_000
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 5 * time.Second
	}
	return c
}

// BufferedSink is the default buffered analytics sink with backpressure.
type BufferedSink struct {
	mu      sync.Mutex
	writer  Writer
	config  Config
	buffer  []AnalyticsEvent
	backoff time.Duration
	metrics Metrics
}

// Metrics receives sink telemetry.
type Metrics interface {
	RecordBuffered(int)
	RecordBacklog(int)
	RecordDropped(int)
}

// NewBufferedSink builds a buffered sink.
func NewBufferedSink(writer Writer, config Config, metrics Metrics) *BufferedSink {
	return &BufferedSink{writer: writer, config: config.withDefaults(), metrics: metrics}
}

// Write buffers an event, flushing when the batch size is reached.
func (s *BufferedSink) Write(_ context.Context, event AnalyticsEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.buffer) >= s.config.BacklogLimit {
		dropped := len(s.buffer) - (s.config.BacklogLimit - 1)
		s.buffer = s.buffer[dropped:]
		if s.metrics != nil {
			s.metrics.RecordDropped(dropped)
		}
	}
	s.buffer = append(s.buffer, event)
	if s.metrics != nil {
		s.metrics.RecordBuffered(len(s.buffer))
	}
	if len(s.buffer) >= s.config.BatchSize {
		s.flushLocked(context.Background())
	}
}

// Flush writes any buffered events now.
func (s *BufferedSink) Flush(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushLocked(ctx)
}

func (s *BufferedSink) flushLocked(ctx context.Context) error {
	if len(s.buffer) == 0 {
		return nil
	}
	batch := s.buffer
	if err := s.writer.WriteBatch(ctx, batch); err != nil {
		if s.backoff == 0 {
			s.backoff = 10 * time.Millisecond
		} else {
			s.backoff *= 2
			if s.backoff > s.config.MaxBackoff {
				s.backoff = s.config.MaxBackoff
			}
		}
		// Drop oldest under pressure to bound memory.
		drop := 0
		for len(s.buffer) > s.config.BacklogLimit {
			s.buffer = s.buffer[1:]
			drop++
		}
		if s.metrics != nil {
			s.metrics.RecordDropped(drop)
		}
		return err
	}
	s.buffer = nil
	s.backoff = 0
	if s.metrics != nil {
		s.metrics.RecordBuffered(0)
	}
	return nil
}

// Pending reports the number of buffered events (diagnostics).
func (s *BufferedSink) Pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.buffer)
}
