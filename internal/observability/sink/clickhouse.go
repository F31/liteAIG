package sink

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
)

// ClickHouseWriter writes analytics batches to a ClickHouse-compatible HTTP
// insert endpoint (JSONEachRow).
type ClickHouseWriter struct {
	client    *http.Client
	insertURL string
	table     string
}

// NewClickHouseWriter builds a writer for an HTTP insert endpoint.
func NewClickHouseWriter(client *http.Client, baseURL, table string) (*ClickHouseWriter, error) {
	if client == nil || baseURL == "" || table == "" {
		return nil, errors.New("clickhouse writer requires a client, base URL, and table")
	}
	return &ClickHouseWriter{client: client, insertURL: baseURL + "/?query=INSERT+INTO+" + table + "+FORMAT+JSONEachRow", table: table}, nil
}

// WriteBatch encodes the batch as JSONEachRow and POSTs it.
func (w *ClickHouseWriter) WriteBatch(ctx context.Context, batch []AnalyticsEvent) error {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	for _, event := range batch {
		if err := encoder.Encode(event); err != nil {
			return err
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, w.insertURL, &buffer)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := w.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return errors.New("clickhouse insert failed")
	}
	return nil
}

// Adapter forwards standard Domain Events into the analytics sink with
// redaction: only whitelisted fields are copied.
type Adapter struct {
	sink AnalyticsSink
	now  func() time.Time
}

// NewAdapter builds an EventSink-compatible adapter.
func NewAdapter(sink AnalyticsSink, now func() time.Time) *Adapter {
	if now == nil {
		now = time.Now
	}
	return &Adapter{sink: sink, now: now}
}

// Emit copies a redacted analytics event from a DomainEvent.
func (a *Adapter) Emit(_ context.Context, event contracts.DomainEvent) error {
	a.sink.Write(context.Background(), AnalyticsEvent{
		ID:         event.ID,
		Kind:       event.Kind,
		TenantID:   event.TenantID,
		ProjectID:  event.ProjectID,
		RequestID:  event.RequestID,
		OccurredAt: a.now().UTC(),
		Attributes: redactAttributes(event.Attributes),
	})
	return nil
}

// redactAttributes copies only low-cardinality, non-secret attributes.
func redactAttributes(attributes map[string]string) map[string]string {
	if len(attributes) == 0 {
		return nil
	}
	result := map[string]string{}
	for key, value := range attributes {
		if sensitiveAttribute(key) {
			continue
		}
		result[key] = value
	}
	return result
}

// sensitiveAttribute reports attribute keys that must never enter analytics.
func sensitiveAttribute(key string) bool {
	switch key {
	case "content", "body", "response_body", "prompt", "secret", "api_key", "authorization", "password", "token":
		return true
	}
	return false
}
