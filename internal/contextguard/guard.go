// Package contextguard implements the P0 pre-request Context Guard (spec
// §12.3): estimate input tokens, compare against the target model context
// window, compute the maximum available output, and reject in strict mode
// before the provider call.
package contextguard

import (
	"encoding/json"
	"errors"

	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/tokenizer"
)

// ErrContextExceeded is returned when strict mode rejects an over-limit
// request.
var ErrContextExceeded = errors.New("context window exceeded")

// Result carries the pre-request context facts.
type Result struct {
	InputTokens        int64
	ContextWindow      int
	MaxAvailableOutput int64
	Exceeded           bool
}

// Guard estimates input tokens and applies the model context window.
type Guard struct {
	registry   *tokenizer.Registry
	strictMode bool
}

// New builds a Context Guard. Strict mode rejects over-limit requests;
// non-strict mode reports the exceeded flag and lets the caller decide.
func New(registry *tokenizer.Registry, strictMode bool) *Guard {
	return &Guard{registry: registry, strictMode: strictMode}
}

// Check evaluates a request against a model's context window. A zero or
// negative context window disables the guard (no constraint configured).
func (g *Guard) Check(request *interaction.UnifiedRequest, model string, contextWindow int) (Result, error) {
	if request == nil || contextWindow <= 0 {
		return Result{}, nil
	}
	input := estimateInputTokens(g.registry, request, model)
	maxOutput := int64(contextWindow) - input
	if maxOutput < 0 {
		maxOutput = 0
	}
	result := Result{
		InputTokens:        input,
		ContextWindow:      contextWindow,
		MaxAvailableOutput: maxOutput,
		Exceeded:           input > int64(contextWindow),
	}
	if g.strictMode && result.Exceeded {
		return result, ErrContextExceeded
	}
	return result, nil
}

// estimateInputTokens sizes the request prompt with the tokenizer registry,
// falling back to a conservative estimate when the model is unknown.
func estimateInputTokens(registry *tokenizer.Registry, request *interaction.UnifiedRequest, model string) int64 {
	if registry == nil {
		registry = tokenizer.New()
	}
	var total int64
	switch {
	case request.Chat != nil:
		for _, message := range request.Chat.Messages {
			total += registry.EstimateTokens(model, message.Role+"\n"+message.Content+"\n"+message.ToolCallID).Tokens
			for _, call := range message.ToolCalls {
				total += registry.EstimateTokens(model, call.Function.Name+"\n"+call.Function.Arguments).Tokens
			}
		}
		if len(request.Chat.Tools) > 0 {
			if bytes, err := json.Marshal(request.Chat.Tools); err == nil {
				total += registry.EstimateTokens(model, string(bytes)).Tokens
			}
		}
	case request.Embedding != nil:
		for _, input := range request.Embedding.Inputs {
			total += registry.EstimateTokens(model, input).Tokens
		}
	case request.Tool != nil:
		total += registry.EstimateTokens(model, request.Tool.Name+"\n"+string(request.Tool.Arguments)+"\n"+request.Tool.Result).Tokens
	}
	return total
}
