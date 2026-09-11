package sink

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type recordingWriter struct {
	mu      sync.Mutex
	batches [][]AnalyticsEvent
	fails   int
	err     error
}

func (w *recordingWriter) WriteBatch(_ context.Context, batch []AnalyticsEvent) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fails > 0 {
		w.fails--
		return w.err
	}
	copy := append([]AnalyticsEvent(nil), batch...)
	w.batches = append(w.batches, copy)
	return nil
}

func event(id string) AnalyticsEvent {
	return AnalyticsEvent{ID: id, Kind: "request.completed", Attributes: map[string]string{}}
}

func TestFlushOnBatchSize(t *testing.T) {
	writer := &recordingWriter{}
	sink := NewBufferedSink(writer, Config{BatchSize: 3, FlushInterval: time.Hour}, nil)
	for i := 0; i < 5; i++ {
		sink.Write(context.Background(), event("e"))
	}
	if len(writer.batches) != 1 || len(writer.batches[0]) != 3 {
		t.Fatalf("batches = %+v", writer.batches)
	}
	if sink.Pending() != 2 {
		t.Fatalf("pending = %d", sink.Pending())
	}
	if err := sink.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(writer.batches) != 2 || len(writer.batches[1]) != 2 {
		t.Fatalf("flush batches = %+v", writer.batches)
	}
}

func TestRetryWithBackoffAndAcknowledgedNotResent(t *testing.T) {
	writer := &recordingWriter{fails: 1, err: errors.New("down")}
	sink := NewBufferedSink(writer, Config{BatchSize: 100, FlushInterval: time.Hour}, nil)
	sink.Write(context.Background(), event("a"))
	sink.Write(context.Background(), event("b"))
	// First flush fails (buffer retained); the next flush retries and succeeds;
	// acknowledged events are not re-sent.
	if err := sink.Flush(context.Background()); err == nil {
		t.Fatal("expected first flush to fail")
	}
	if sink.Pending() != 2 {
		t.Fatalf("failed flush lost the buffer: pending=%d", sink.Pending())
	}
	if err := sink.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(writer.batches) != 1 || len(writer.batches[0]) != 2 {
		t.Fatalf("batches = %+v", writer.batches)
	}
	if sink.backoff != 0 {
		t.Fatalf("backoff not reset after success: %v", sink.backoff)
	}
}

func TestBacklogPressureDropsOldest(t *testing.T) {
	writer := &recordingWriter{err: errors.New("down")}
	metrics := &countingMetrics{}
	sink := NewBufferedSink(writer, Config{BatchSize: 100, FlushInterval: time.Hour, BacklogLimit: 3}, metrics)
	for i := 0; i < 6; i++ {
		sink.Write(context.Background(), event("e"))
	}
	// With a limit of 3, the oldest 3 are dropped; pending is bounded at 3.
	if sink.Pending() > 3 {
		t.Fatalf("pending = %d, unbounded backlog", sink.Pending())
	}
	if metrics.dropped == 0 {
		t.Fatal("drop-oldest not recorded")
	}
}

type countingMetrics struct {
	mu      sync.Mutex
	dropped int
}

func (m *countingMetrics) RecordBuffered(int) {}
func (m *countingMetrics) RecordBacklog(int)  {}
func (m *countingMetrics) RecordDropped(n int) {
	m.mu.Lock()
	m.dropped += n
	m.mu.Unlock()
}
