package guardrail

import (
	"context"
	"strings"
	"testing"

	"github.com/F31/liteAIG/internal/guardrail/builtin"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/interaction"
)

func toolEngine(t *testing.T, action string) *builtin.Engine {
	t.Helper()
	engine, err := builtin.New(builtin.Policy{Rules: []builtin.Rule{{ID: "ssn", Kind: "pii", Pattern: `[0-9]{3}-[0-9]{2}-[0-9]{4}`, Action: action}}})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func TestOutputToolCallArgumentsBlock(t *testing.T) {
	store := &securityStore{}
	handler := Handler{Engine: toolEngine(t, "block"), Output: true, SecurityEvents: store, IDs: ids{}, Clock: clock{}}
	request := &kernel.RequestContext{
		RequestID:   "r",
		Interaction: &interaction.Context{TenantID: "t", ProjectID: "p"},
		Response: &interaction.UnifiedResponse{Choices: []interaction.Choice{{Index: 0, Message: interaction.Message{
			Role:      "assistant",
			ToolCalls: []interaction.ToolCall{{ID: "call_1", Function: interaction.ToolCallFunction{Name: "pay", Arguments: `{"ssn":"123-45-6789"}`}}},
		}}}},
	}
	if _, err := handler.Handle(context.Background(), request); err == nil {
		t.Fatal("expected a block on secret tool call arguments")
	} else if !strings.Contains(err.Error(), "blocked") && !strings.Contains(err.Error(), "GUARDRAIL_BLOCKED") {
		t.Fatalf("err=%v", err)
	}
	if len(store.events) != 1 || store.events[0].RuleID != "ssn" {
		t.Fatalf("events=%+v", store.events)
	}
}

func TestOutputToolCallArgumentsRedactEscalatesToBlock(t *testing.T) {
	store := &securityStore{}
	handler := Handler{Engine: toolEngine(t, "redact"), Output: true, SecurityEvents: store, IDs: ids{}, Clock: clock{}}
	arguments := `{"ssn":"123-45-6789"}`
	request := &kernel.RequestContext{
		RequestID:   "r",
		Interaction: &interaction.Context{TenantID: "t", ProjectID: "p"},
		Response: &interaction.UnifiedResponse{Choices: []interaction.Choice{{Index: 0, Message: interaction.Message{
			Role:      "assistant",
			ToolCalls: []interaction.ToolCall{{ID: "call_1", Function: interaction.ToolCallFunction{Name: "pay", Arguments: arguments}}},
		}}}},
	}
	if _, err := handler.Handle(context.Background(), request); err == nil {
		t.Fatal("redact on arguments must escalate to a block")
	}
	// The arguments must be left untouched: splicing would break the JSON.
	if request.Response.Choices[0].Message.ToolCalls[0].Function.Arguments != arguments {
		t.Fatalf("arguments mutated: %s", request.Response.Choices[0].Message.ToolCalls[0].Function.Arguments)
	}
	if len(store.events) != 1 {
		t.Fatalf("events=%+v", store.events)
	}
}

func TestInputHistoryToolCallArgumentsBlock(t *testing.T) {
	store := &securityStore{}
	handler := Handler{Engine: toolEngine(t, "block"), SecurityEvents: store, IDs: ids{}, Clock: clock{}}
	request := &kernel.RequestContext{
		RequestID:   "r",
		Interaction: &interaction.Context{TenantID: "t", ProjectID: "p"},
		Request: &interaction.UnifiedRequest{Chat: &interaction.ChatPayload{Messages: []interaction.Message{
			{Role: "assistant", Content: "", ToolCalls: []interaction.ToolCall{{ID: "call_1", Function: interaction.ToolCallFunction{Name: "pay", Arguments: `{"ssn":"123-45-6789"}`}}}},
			{Role: "tool", ToolCallID: "call_1", Content: "ok"},
		}}},
	}
	if _, err := handler.Handle(context.Background(), request); err == nil {
		t.Fatal("expected a block on echoed secret tool call arguments")
	}
}

func TestToolResultHistoryStillScanned(t *testing.T) {
	store := &securityStore{}
	handler := Handler{Engine: toolEngine(t, "block"), SecurityEvents: store, IDs: ids{}, Clock: clock{}}
	request := &kernel.RequestContext{
		RequestID:   "r",
		Interaction: &interaction.Context{TenantID: "t", ProjectID: "p"},
		Request: &interaction.UnifiedRequest{Chat: &interaction.ChatPayload{Messages: []interaction.Message{
			{Role: "tool", ToolCallID: "call_1", Content: "leaked 123-45-6789"},
		}}},
	}
	if _, err := handler.Handle(context.Background(), request); err == nil {
		t.Fatal("role:tool content must be scanned as untrusted input")
	}
}

func TestCleanToolCallsPass(t *testing.T) {
	handler := Handler{Engine: toolEngine(t, "block"), Output: true, SecurityEvents: &securityStore{}, IDs: ids{}, Clock: clock{}}
	request := &kernel.RequestContext{RequestID: "r", Interaction: &interaction.Context{TenantID: "t", ProjectID: "p"}, Response: &interaction.UnifiedResponse{Choices: []interaction.Choice{{Index: 0, Message: interaction.Message{Role: "assistant", Content: "calling", ToolCalls: []interaction.ToolCall{{ID: "call_1", Function: interaction.ToolCallFunction{Name: "pay", Arguments: `{"amount":5}`}}}}}}}}
	if _, err := handler.Handle(context.Background(), request); err != nil {
		t.Fatalf("clean tool call must pass: %v", err)
	}
}
