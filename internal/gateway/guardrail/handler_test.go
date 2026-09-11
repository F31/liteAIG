package guardrail

import (
	"context"
	"errors"
	"testing"
	"time"

	guardraildomain "github.com/F31/liteAIG/internal/guardrail"
	"github.com/F31/liteAIG/internal/guardrail/builtin"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/tenancy"
)

type securityStore struct {
	events []guardraildomain.SecurityEvent
}

func (s *securityStore) Create(_ context.Context, _ tenancy.TenantScope, event guardraildomain.SecurityEvent) error {
	s.events = append(s.events, event)
	return nil
}
func (s *securityStore) List(context.Context, tenancy.TenantScope, int) ([]guardraildomain.SecurityEvent, error) {
	return s.events, nil
}

type ids struct{}

func (ids) New() (string, error) { return "event", nil }

type clock struct{}

func (clock) Now() time.Time { return time.Unix(1, 0) }

type sink struct{ event contracts.DomainEvent }

func (s *sink) Emit(_ context.Context, event contracts.DomainEvent) error {
	s.event = event
	return errors.New("sink down")
}
func TestGuardrailEnforcementDoesNotDependOnSink(t *testing.T) {
	engine, _ := builtin.New(builtin.Policy{Rules: []builtin.Rule{{ID: "secret", Kind: "keyword", Pattern: "secret", Action: "redact"}}})
	sink := &sink{}
	handler := Handler{Engine: engine, Sink: sink, IDs: ids{}, Clock: clock{}}
	request := &kernel.RequestContext{RequestID: "r", Interaction: &interaction.Context{TenantID: "t", ProjectID: "p"}, Request: &interaction.UnifiedRequest{Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "a secret"}}}}}
	if _, err := handler.Handle(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if request.Request.Chat.Messages[0].Content != "a [redacted]" || sink.event.Attributes["content_hash"] == "" {
		t.Fatalf("request=%+v event=%+v", request.Request, sink.event)
	}
}

func TestGuardrailPersistsRedactedSecurityEvent(t *testing.T) {
	engine, _ := builtin.New(builtin.Policy{Rules: []builtin.Rule{{ID: "pii", Kind: "pii", Pattern: `[0-9]{3}-[0-9]{2}`, Action: "redact"}}})
	store := &securityStore{}
	handler := Handler{Engine: engine, SecurityEvents: store, IDs: ids{}, Clock: clock{}}
	request := &kernel.RequestContext{RequestID: "r", Interaction: &interaction.Context{TenantID: "t", ProjectID: "p"}, Request: &interaction.UnifiedRequest{Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "value 123-45"}}}}}
	if _, err := handler.Handle(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if len(store.events) != 1 || store.events[0].ContentHash == "" || store.events[0].RuleID != "pii" {
		t.Fatalf("events = %+v", store.events)
	}
	if store.events[0].ContentHash == "value 123-45" {
		t.Fatal("security event persisted raw content")
	}
}

func TestToolResultIsUntrustedAndReGuardrailed(t *testing.T) {
	engine, _ := builtin.New(builtin.Policy{Rules: []builtin.Rule{{ID: "secret", Kind: "keyword", Pattern: "secret", Action: "redact"}}})
	handler := Handler{Engine: engine, Output: true}
	request := &kernel.RequestContext{Interaction: &interaction.Context{TenantID: "t", ProjectID: "p"}, Response: &interaction.UnifiedResponse{ToolResult: &interaction.ToolResult{Name: "tool", Content: "secret value"}}}
	if _, err := handler.Handle(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if request.Response.ToolResult.Content != "[redacted] value" || request.Response.ToolResult.Provenance.Source != "tool_result" || request.Response.ToolResult.Provenance.Trusted {
		t.Fatalf("tool result = %+v", request.Response.ToolResult)
	}
}

func TestFileUploadInputIsGuardrailed(t *testing.T) {
	engine, _ := builtin.New(builtin.Policy{Rules: []builtin.Rule{{ID: "secret", Kind: "keyword", Pattern: "secret", Action: "redact"}}})
	handler := Handler{Engine: engine, Sink: &sink{}, IDs: ids{}, Clock: clock{}}
	request := &kernel.RequestContext{RequestID: "r", Interaction: &interaction.Context{TenantID: "t", ProjectID: "p"}, Request: &interaction.UnifiedRequest{Kind: interaction.RequestFile, File: &interaction.FilePayload{Operation: "upload", Data: []byte(`{"body":"secret"}`)}}}
	if _, err := handler.Handle(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if string(request.Request.File.Data) != `{"body":"[redacted]"}` {
		t.Fatalf("file data = %s", request.Request.File.Data)
	}
}

func TestFileUploadInputBlockStopsBatchArtifact(t *testing.T) {
	engine, _ := builtin.New(builtin.Policy{Rules: []builtin.Rule{{ID: "forbidden", Kind: "keyword", Pattern: "forbidden", Action: "block"}}})
	handler := Handler{Engine: engine, Sink: &sink{}, IDs: ids{}, Clock: clock{}}
	request := &kernel.RequestContext{RequestID: "r", Interaction: &interaction.Context{TenantID: "t", ProjectID: "p"}, Request: &interaction.UnifiedRequest{Kind: interaction.RequestFile, File: &interaction.FilePayload{Operation: "upload", Data: []byte("forbidden")}}}
	if _, err := handler.Handle(context.Background(), request); err == nil {
		t.Fatal("blocked file upload returned nil error")
	}
}
