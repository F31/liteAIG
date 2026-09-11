// Package groundedness evaluates whether a model response is grounded in the
// provided context. The default OverlapChecker uses deterministic content
// signals and needs no external model; a model judge may back the same contract.
package groundedness

import (
	"context"
	"strings"
)

// Verdict is the groundedness outcome.
type Verdict struct {
	Passed   bool
	Reason   string
	Evidence string
}

// Checker evaluates a response against context.
type Checker interface {
	Check(context.Context, string, string) (Verdict, error)
}

// OverlapChecker flags responses that contradict or drift from the context using
// lexical overlap and contrast signals. Deterministic and provider-free.
type OverlapChecker struct {
	MinOverlap float64  // minimum token-overlap ratio to be considered grounded
	Contrast   []string // contrast/negation signals
}

// NewOverlapChecker returns a baseline checker with explicit defaults.
func NewOverlapChecker(minOverlap float64) *OverlapChecker {
	if minOverlap <= 0 {
		minOverlap = 0.2
	}
	return &OverlapChecker{
		MinOverlap: minOverlap,
		Contrast:   []string{"however", "on the contrary", "in fact", "actually", "not", "contradicts"},
	}
}

// Check returns a grounded verdict.
func (c *OverlapChecker) Check(_ context.Context, response, context string) (Verdict, error) {
	if strings.TrimSpace(context) == "" {
		return Verdict{Passed: true, Reason: "no context to ground against"}, nil
	}
	contextTokens := tokenize(context)
	if len(contextTokens) == 0 {
		return Verdict{Passed: true, Reason: "no context tokens"}, nil
	}
	contextSet := map[string]bool{}
	for _, token := range contextTokens {
		contextSet[token] = true
	}
	overlap := overlapRatio(response, contextSet)
	contrast := containsAny(response, c.Contrast)
	novelRatio := novelRatio(response, contextSet)
	// A strong contrast signal plus substantial new claims reads as a
	// contradiction of the context.
	if contrast && novelRatio >= 0.3 {
		return Verdict{Passed: false, Reason: "response contradicts context", Evidence: "contrast+novel"}, nil
	}
	if overlap < c.MinOverlap && len(tokenize(response)) > 3 {
		return Verdict{Passed: false, Reason: "response not grounded in context", Evidence: "low-overlap"}, nil
	}
	return Verdict{Passed: true, Reason: "grounded in context", Evidence: "overlap"}, nil
}

func tokenize(value string) []string {
	return strings.Fields(strings.ToLower(value))
}

func overlapRatio(response string, contextSet map[string]bool) float64 {
	responseTokens := tokenize(response)
	if len(responseTokens) == 0 {
		return 0
	}
	matched := 0
	for _, token := range responseTokens {
		if contextSet[token] {
			matched++
		}
	}
	return float64(matched) / float64(len(responseTokens))
}

func novelRatio(response string, contextSet map[string]bool) float64 {
	responseTokens := tokenize(response)
	if len(responseTokens) == 0 {
		return 0
	}
	novel := 0
	for _, token := range responseTokens {
		if !contextSet[token] {
			novel++
		}
	}
	return float64(novel) / float64(len(responseTokens))
}

func containsAny(value string, signals []string) bool {
	lower := strings.ToLower(value)
	for _, signal := range signals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}
