package golden

import (
	"context"
	"testing"

	"github.com/F31/liteAIG/internal/gateway/guardrail"
	guardraildomain "github.com/F31/liteAIG/internal/guardrail"
	"github.com/F31/liteAIG/internal/guardrail/builtin"
	"github.com/F31/liteAIG/internal/guardrail/groundedness"
	"github.com/F31/liteAIG/internal/guardrail/judge"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/tenancy"
)

func TestScenarioDAdvancedOutputGuardrailRedacted(t *testing.T) {
	engine, _ := builtin.New(builtin.Policy{})
	store := &advancedEventStore{}
	handler := guardrail.Handler{
		Engine: engine, Output: true, IDs: idsGen{}, Clock: sysClock{},
		Groundedness:   groundedness.NewOverlapChecker(0.5),
		Judge:          judge.NewMockJudge([]string{"dangerous"}),
		Action:         "mark",
		SecurityEvents: store,
	}
	request := &kernel.RequestContext{
		RequestID: "r", Interaction: &interaction.Context{TenantID: "t", ProjectID: "p"},
		Response: &interaction.UnifiedResponse{Choices: []interaction.Choice{{Message: interaction.Message{Role: "assistant", Content: "on the contrary, the sky is green"}}}},
		Request:  &interaction.UnifiedRequest{Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "the sky is blue and clear"}}}},
	}
	if _, err := handler.Handle(context.Background(), request); err != nil {
		t.Fatalf("mark action must not block: %v", err)
	}
	if len(store.events) == 0 {
		t.Fatal("no security event for advanced failure")
	}
	// The event must be redacted: a content hash, never the raw response.
	raw := "on the contrary, the sky is green"
	for _, event := range store.events {
		if event.ContentHash == raw || event.ContentHash == "" {
			t.Fatalf("event leaked raw content: %+v", event)
		}
	}
}

type advancedEventStore struct {
	events []guardraildomain.SecurityEvent
}

func (s *advancedEventStore) Create(_ context.Context, _ tenancy.TenantScope, event guardraildomain.SecurityEvent) error {
	s.events = append(s.events, event)
	return nil
}
func (s *advancedEventStore) List(context.Context, tenancy.TenantScope, int) ([]guardraildomain.SecurityEvent, error) {
	return s.events, nil
}
