package guardrail

import (
	"context"
	"testing"

	"github.com/F31/liteAIG/internal/guardrail/builtin"
	"github.com/F31/liteAIG/internal/guardrail/groundedness"
	"github.com/F31/liteAIG/internal/guardrail/judge"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/interaction"
)

func advancedOutputHandler(action string) Handler {
	engine, _ := builtin.New(builtin.Policy{})
	return Handler{
		Engine: engine, Output: true, IDs: ids{}, Clock: clock{},
		Groundedness:   groundedness.NewOverlapChecker(0.5),
		Judge:          judge.NewMockJudge([]string{"dangerous"}),
		Action:         action,
		SecurityEvents: &securityStore{},
	}
}

func advancedRequest(response, input string) *kernel.RequestContext {
	return &kernel.RequestContext{
		RequestID: "r", Interaction: &interaction.Context{TenantID: "t", ProjectID: "p"},
		Response: &interaction.UnifiedResponse{Choices: []interaction.Choice{{Message: interaction.Message{Role: "assistant", Content: response}}}},
		Request:  &interaction.UnifiedRequest{Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: input}}}},
	}
}

func TestAdvancedBlockRejectsBeforeRelease(t *testing.T) {
	handler := advancedOutputHandler("block")
	request := advancedRequest("on the contrary, the sky is green", "the sky is blue and clear")
	_, err := handler.Handle(context.Background(), request)
	if err == nil {
		t.Fatal("blocked advanced failure not rejected")
	}
}

func TestAdvancedMarkRecordsRedactedEvent(t *testing.T) {
	handler := advancedOutputHandler("mark")
	store := handler.SecurityEvents.(*securityStore)
	request := advancedRequest("on the contrary, the sky is green", "the sky is blue and clear")
	if _, err := handler.Handle(context.Background(), request); err != nil {
		t.Fatalf("mark action must not block: %v", err)
	}
	if len(store.events) == 0 {
		t.Fatal("no redacted security event recorded")
	}
	// The event carries a hash, never the raw response.
	for _, event := range store.events {
		if event.ContentHash == "on the contrary, the sky is green" || event.ContentHash == "" {
			t.Fatalf("event leaked raw content: %+v", event)
		}
	}
}

func TestAdvancedRetroactiveDoesNotBlock(t *testing.T) {
	handler := advancedOutputHandler("retroactive")
	request := advancedRequest("on the contrary, the sky is green", "the sky is blue and clear")
	if _, err := handler.Handle(context.Background(), request); err != nil {
		t.Fatalf("retroactive action must not block: %v", err)
	}
}

func TestAdvancedUnconfiguredStageUnchanged(t *testing.T) {
	engine, _ := builtin.New(builtin.Policy{})
	handler := Handler{Engine: engine, Output: true}
	request := advancedRequest("on the contrary, the sky is green", "the sky is blue and clear")
	if _, err := handler.Handle(context.Background(), request); err != nil {
		t.Fatalf("unconfigured stage must not block: %v", err)
	}
}
