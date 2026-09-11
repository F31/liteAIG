package tokenizer

import (
	"strings"
	"testing"
)

func TestEstimateTokensKnownModel(t *testing.T) {
	r := New()
	// claude family is a known encoding; ~3.8 chars/token.
	text := strings.Repeat("a", 380)
	res := r.EstimateTokens("claude-3-5-sonnet-20241022", text)
	if res.Method != MethodLocal {
		t.Fatalf("method=%s want local", res.Method)
	}
	if res.Tokens != 100 {
		t.Fatalf("tokens=%d want 100 for 380 chars at 3.8 chars/token", res.Tokens)
	}
}

func TestEstimateTokensUnknownModelConservative(t *testing.T) {
	r := New()
	res := r.EstimateTokens("future-model-2099", "hello world, this is some text")
	if res.Method != MethodConservative {
		t.Fatalf("method=%s want conservative", res.Method)
	}
	if res.Tokens < 1 {
		t.Fatalf("tokens=%d", res.Tokens)
	}
}

func TestEstimateTokensSuffixMatch(t *testing.T) {
	r := New()
	// "o1" is a suffix of "o1-preview"; longest match must win and not be
	// confused with another family.
	if res := r.EstimateTokens("o1-preview-2024-09-12", ""); res.Tokens != 1 {
		t.Fatalf("tokens=%d", res.Tokens)
	}
	if res := r.EstimateTokens("gpt-4o-mini", "aaaaaaaaaa"); res.Method != MethodLocal {
		t.Fatalf("gpt-4o-mini method=%s", res.Method)
	}
}

func TestEstimateTokensEmptyText(t *testing.T) {
	r := New()
	res := r.EstimateTokens("unknown-model", "")
	if res.Tokens != 1 {
		t.Fatalf("tokens=%d want 1", res.Tokens)
	}
	if res.Method != MethodConservative {
		t.Fatalf("method=%s", res.Method)
	}
}

func TestRegistryVersionStable(t *testing.T) {
	if New().Version() != RegistryVersion {
		t.Fatalf("version mismatch")
	}
}
