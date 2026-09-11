package contextguard

import (
	"errors"
	"strings"
	"testing"

	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/tokenizer"
)

func TestCheckWithinWindow(t *testing.T) {
	g := New(tokenizer.New(), true)
	req := &interaction.UnifiedRequest{
		Model: "claude-3-5-sonnet",
		Chat:  &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: strings.Repeat("a", 100)}}},
	}
	result, err := g.Check(req, "claude-3-5-sonnet", 200000)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if result.Exceeded {
		t.Fatal("short request reported exceeded")
	}
	if result.InputTokens < 1 || result.InputTokens >= int64(result.ContextWindow) {
		t.Fatalf("input=%d window=%d", result.InputTokens, result.ContextWindow)
	}
	if result.MaxAvailableOutput != int64(result.ContextWindow)-result.InputTokens {
		t.Fatalf("maxAvailable=%d", result.MaxAvailableOutput)
	}
}

func TestCheckExceedsWindowStrict(t *testing.T) {
	g := New(tokenizer.New(), true)
	req := &interaction.UnifiedRequest{
		Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: strings.Repeat("a", 1000000)}}},
	}
	_, err := g.Check(req, "some-model", 200000)
	if !errors.Is(err, ErrContextExceeded) {
		t.Fatalf("err=%v want ErrContextExceeded", err)
	}
}

func TestCheckExceedsWindowPermissive(t *testing.T) {
	g := New(tokenizer.New(), false)
	req := &interaction.UnifiedRequest{
		Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: strings.Repeat("a", 1000000)}}},
	}
	result, err := g.Check(req, "some-model", 200000)
	if err != nil {
		t.Fatalf("permissive mode must not error: %v", err)
	}
	if !result.Exceeded {
		t.Fatal("expected exceeded=true")
	}
	if result.MaxAvailableOutput != 0 {
		t.Fatalf("maxAvailable=%d want 0", result.MaxAvailableOutput)
	}
}

func TestCheckDisabledOnZeroWindow(t *testing.T) {
	g := New(tokenizer.New(), true)
	result, err := g.Check(&interaction.UnifiedRequest{Chat: &interaction.ChatPayload{}}, "m", 0)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if result.ContextWindow != 0 {
		t.Fatalf("window=%d", result.ContextWindow)
	}
}

func TestCheckNilRequest(t *testing.T) {
	g := New(tokenizer.New(), true)
	if _, err := g.Check(nil, "m", 1000); err != nil {
		t.Fatalf("err=%v", err)
	}
}
