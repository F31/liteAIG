// Package-level structured security/operational event logging bridge: a
// redacted domain event is exported to the OTLP logs channel with a fixed
// attribute allowlist. Raw event attributes are never forwarded wholesale.
package observability

import (
	"context"
	"strings"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
)

// eventLogAllowlist is the fixed set of DomainEvent attribute keys allowed
// into structured logs. Everything else (even if a producer added it) is
// dropped so the exported log stays sanitized.
var eventLogAllowlist = map[string]bool{
	"action":        true,
	"rule_id":       true,
	"severity":      true,
	"status":        true,
	"operation":     true,
	"file_id":       true,
	"logical_model": true,
	"deployment_id": true,
	"outcome":       true,
	"content_hash":  true,
	"reason":        true,
	"decision":      true,
	"account_id":    true,
}

// securityEventPrefixes map to a "warning" severity when the event carries no
// explicit severity attribute.
var securityEventPrefixes = []string{
	"guardrail.",
	"file.access",
	"system_config.update",
	"approval.rejected",
	"alert.",
	"audit.",
	"admin.",
}

// StructuredEventLog converts a redacted DomainEvent into an OTLP LogRecord
// using only the allowlisted attributes. It returns false for empty/noop
// events, which callers should skip.
func StructuredEventLog(event contracts.DomainEvent) (LogRecord, bool) {
	if event.ID == "" || event.Kind == "" {
		return LogRecord{}, false
	}
	attributes := make(map[string]string, len(event.Attributes)+4)
	for key, value := range event.Attributes {
		if eventLogAllowlist[key] && value != "" {
			attributes[key] = value
		}
	}
	if event.TenantID != "" {
		attributes["tenant_id"] = event.TenantID
	}
	if event.ProjectID != "" {
		attributes["project_id"] = event.ProjectID
	}
	if event.RequestID != "" {
		attributes["request_id"] = event.RequestID
	}
	attributes["event_id"] = event.ID
	severity := attributes["severity"]
	if severity == "" {
		severity = defaultEventSeverity(event.Kind)
	}
	occurredAt := event.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}
	return LogRecord{
		Message:    "liteaig.events." + event.Kind,
		Severity:   severity,
		Attributes: attributes,
		Timestamp:  occurredAt,
	}, true
}

func defaultEventSeverity(kind string) string {
	for _, prefix := range securityEventPrefixes {
		if strings.HasPrefix(kind, prefix) {
			return "warning"
		}
	}
	return "info"
}

// OTLPLogSink adapts an OTLPLogger to the EventSink contract, exporting only
// allowlisted structured events. Emit is best-effort: export failures never
// fail the caller.
type OTLPLogSink struct {
	Logger *OTLPLogger
}

func (s OTLPLogSink) Emit(ctx context.Context, event contracts.DomainEvent) error {
	if s.Logger == nil {
		return nil
	}
	record, ok := StructuredEventLog(event)
	if !ok {
		return nil
	}
	return s.Logger.Emit(ctx, record)
}

var _ contracts.EventSink = OTLPLogSink{}
