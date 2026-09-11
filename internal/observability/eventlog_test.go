package observability

import (
	"context"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
)

func TestStructuredEventLogAllowlistsAttributes(t *testing.T) {
	event := contracts.DomainEvent{
		ID:         "event-1",
		Kind:       "guardrail.match",
		TenantID:   "tenant-1",
		ProjectID:  "project-1",
		RequestID:  "request-1",
		OccurredAt: time.Unix(100, 0),
		Attributes: map[string]string{
			"rule_id":  "deny",
			"action":   "block",
			"prompt":   "sensitive",
			"response": "sensitive",
			"secret":   "sk-super-secret",
			"unknown":  "sensitive",
		},
	}
	record, ok := StructuredEventLog(event)
	if !ok {
		t.Fatal("event rejected")
	}
	if record.Timestamp != event.OccurredAt || record.Message != "liteaig.events.guardrail.match" {
		t.Fatalf("record = %+v", record)
	}
	if record.Severity != "warning" {
		t.Fatalf("severity = %q", record.Severity)
	}
	for _, key := range []string{"prompt", "response", "secret", "unknown"} {
		if _, exists := record.Attributes[key]; exists {
			t.Fatalf("attribute %q leaked into structured log: %+v", key, record.Attributes)
		}
	}
	for _, key := range []string{"rule_id", "action", "tenant_id", "project_id", "request_id", "event_id"} {
		if record.Attributes[key] == "" {
			t.Fatalf("allowlisted key %q missing: %+v", key, record.Attributes)
		}
	}
	for value := range record.Attributes {
		if value == "sk-super-secret" {
			t.Fatalf("secret value leaked: %+v", record.Attributes)
		}
	}
}

func TestStructuredEventLogRejectsNoopAndMapsSeverity(t *testing.T) {
	if _, ok := StructuredEventLog(contracts.DomainEvent{}); ok {
		t.Fatal("empty event must be rejected")
	}
	infoRecord, ok := StructuredEventLog(contracts.DomainEvent{ID: "event", Kind: "request.completed"})
	if !ok || infoRecord.Severity != "info" {
		t.Fatalf("info event = %+v", infoRecord)
	}
	firing, _ := StructuredEventLog(contracts.DomainEvent{ID: "event", Kind: "alert.firing"})
	if firing.Severity != "warning" {
		t.Fatalf("alert severity = %q", firing.Severity)
	}
	explicit, _ := StructuredEventLog(contracts.DomainEvent{ID: "event", Kind: "request.completed", Attributes: map[string]string{"severity": "error", "outcome": "error"}})
	if explicit.Severity != "error" || explicit.Attributes["outcome"] != "error" {
		t.Fatalf("explicit severity = %+v", explicit)
	}
}

func TestOTLPLogSinkSkipsNilAndNoop(t *testing.T) {
	// Nil logger: no-op, no panic.
	sink := OTLPLogSink{}
	if err := sink.Emit(context.Background(), contracts.DomainEvent{ID: "event", Kind: "guardrail.match"}); err != nil {
		t.Fatal(err)
	}
}

func TestStructuredEventLogCarriesAccountContext(t *testing.T) {
	event := contracts.DomainEvent{
		ID: "event-1", Kind: "admin.security.reauth_failed", TenantID: "tenant-1",
		Attributes: map[string]string{"account_id": "admin-1", "reason": "password_mismatch", "session_token": "sensitive"},
	}
	record, ok := StructuredEventLog(event)
	if !ok {
		t.Fatal("event rejected")
	}
	if record.Severity != "warning" {
		t.Fatalf("severity = %q", record.Severity)
	}
	if record.Attributes["account_id"] != "admin-1" || record.Attributes["reason"] != "password_mismatch" {
		t.Fatalf("attributes = %+v", record.Attributes)
	}
	if _, exists := record.Attributes["session_token"]; exists {
		t.Fatalf("non-allowlisted attribute leaked: %+v", record.Attributes)
	}
}
