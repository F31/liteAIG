package sink

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
)

func TestAdapterRedactsSensitiveAttributes(t *testing.T) {
	recorded := &recordingSink{}
	adapter := NewAdapter(recorded, func() time.Time { return time.Unix(1, 0) })
	_ = adapter.Emit(context.Background(), contracts.DomainEvent{
		ID: "evt-1", Kind: "request.completed", TenantID: "t", ProjectID: "p", RequestID: "r",
		Attributes: map[string]string{"logical_model": "chat", "content": "the secret body", "authorization": "Bearer x"},
	})
	if len(recorded.events) != 1 {
		t.Fatalf("events = %d", len(recorded.events))
	}
	attributes := recorded.events[0].Attributes
	if attributes["logical_model"] != "chat" {
		t.Fatalf("whitelisted attribute dropped: %+v", attributes)
	}
	if _, ok := attributes["content"]; ok {
		t.Fatal("content leaked into analytics")
	}
	if _, ok := attributes["authorization"]; ok {
		t.Fatal("authorization leaked into analytics")
	}
}

func TestClickHouseWriterHTTPInsert(t *testing.T) {
	var received []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buffer := make([]byte, 4096)
		n, _ := r.Body.Read(buffer)
		received = append(received, buffer[:n]...)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	writer, err := NewClickHouseWriter(server.Client(), server.URL, "analytics")
	if err != nil {
		t.Fatal(err)
	}
	batch := []AnalyticsEvent{{ID: "e1", Kind: "request.completed", Attributes: map[string]string{"logical_model": "chat"}}}
	if err := writer.WriteBatch(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	if len(received) == 0 {
		t.Fatal("no insert body received")
	}
}

type recordingSink struct {
	events []AnalyticsEvent
}

func (s *recordingSink) Write(_ context.Context, event AnalyticsEvent) {
	s.events = append(s.events, event)
}
func (s *recordingSink) Flush(context.Context) error { return nil }
