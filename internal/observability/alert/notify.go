package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	neturl "net/url"
	"strconv"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/platform/egress"
)

// WebhookNotifier delivers alerts to a configured Webhook URL (no secrets).
type WebhookNotifier struct {
	URL string
}

func (n WebhookNotifier) Notify(ctx context.Context, alert Alert) error {
	if n.URL == "" {
		return nil
	}
	sink, err := NewWebhookSink(n.URL)
	if err != nil {
		return err
	}
	defer sink.Close()
	return sink.Emit(ctx, contracts.DomainEvent{ID: alert.ID, Kind: "alert.firing", OccurredAt: alert.FiredAt, TenantID: alert.TenantID,
		Attributes: map[string]string{"rule_id": alert.RuleID, "severity": alert.Severity, "status": alert.Status}})
}

// WebhookSink sends only notification metadata, never arbitrary event attributes.
// Emit is synchronous and bounded; the app queues it off the request path.
type WebhookSink struct {
	url    string
	client *http.Client
}

func NewWebhookSink(url string) (*WebhookSink, error) {
	if url == "" {
		return &WebhookSink{}, nil
	}
	parsed, err := neturl.Parse(url)
	if err != nil {
		return nil, err
	}
	if parsed.User != nil {
		return nil, errors.New("webhook url userinfo is not allowed")
	}
	policy := egress.LitePolicy()
	if err := egress.ValidateTarget(url, policy); err != nil {
		return nil, err
	}
	return &WebhookSink{url: url, client: egress.Client(policy)}, nil
}

// NotificationEvent copies an explicit metadata allowlist before asynchronous use.
func NotificationEvent(event contracts.DomainEvent) (contracts.DomainEvent, bool) {
	switch event.Kind {
	case "alert.firing", "alert.ack", "alert.silence", "alert.resolve",
		"approval.created", "approval.approved", "approval.rejected", "approval.partial",
		"guardrail.match", "guardrail.retroactive", "system_config.update":
	default:
		return contracts.DomainEvent{}, false
	}
	attributes := make(map[string]string)
	for _, key := range []string{"rule_id", "severity", "status", "action", "content_hash"} {
		if value, ok := event.Attributes[key]; ok {
			attributes[key] = value
		}
	}
	event.Attributes = attributes
	return event, true
}

func (s *WebhookSink) Emit(ctx context.Context, event contracts.DomainEvent) error {
	if s.url == "" {
		return nil
	}
	event, ok := NotificationEvent(event)
	if !ok {
		return nil
	}
	payload, err := json.Marshal(struct {
		ID         string            `json:"id"`
		Kind       string            `json:"kind"`
		OccurredAt time.Time         `json:"occurred_at"`
		TenantID   string            `json:"tenant_id"`
		ProjectID  string            `json:"project_id,omitempty"`
		RequestID  string            `json:"request_id,omitempty"`
		Attributes map[string]string `json:"attributes"`
	}{event.ID, event.Kind, event.OccurredAt, event.TenantID, event.ProjectID, event.RequestID, event.Attributes})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &webhookError{status: response.StatusCode}
	}
	return nil
}

func (s *WebhookSink) Close() {
	if s.client != nil {
		s.client.CloseIdleConnections()
	}
}

type webhookError struct{ status int }

func (e *webhookError) Error() string {
	return "webhook notification failed with status " + strconv.Itoa(e.status)
}
