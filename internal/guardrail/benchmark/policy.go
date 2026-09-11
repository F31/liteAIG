package benchmark

import "github.com/F31/liteAIG/internal/guardrail/builtin"

// DefaultPolicy is the fixed, versioned rule set the benchmark runs against.
// The engine version reported in each report is this policy's Version, so a
// guardrail rule change must bump it to keep the regression comparison honest.
func DefaultPolicy() builtin.Policy {
	return builtin.Policy{
		Version: 1,
		Rules: []builtin.Rule{
			{ID: "inj-ignore-prior", Kind: "keyword", Pattern: "ignore your previous instructions", Action: "block"},
			{ID: "inj-override-rules", Kind: "regex", Pattern: `(?i)\b(?:ignore|override|disregard|forget|bypass)\b.{0,40}\b(?:previous|prior|all)?\s*(?:instructions|guardrails|rules|system prompt)\b`, Action: "block"},
			{ID: "secret-api-key", Kind: "regex", Pattern: `(?i)\bsk-\w{16,}\b`, Action: "block"},
			{ID: "pii-email", Kind: "regex", Pattern: `(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`, Action: "redact"},
			{ID: "pii-phone", Kind: "regex", Pattern: `(?i)(?:\+?\d{1,3}[-. ]?)?\(?\d{3}\)?[-. ]?\d{3}[-. ]?\d{4}`, Action: "redact"},
			{ID: "pii-ssn", Kind: "regex", Pattern: `\b\d{3}-\d{2}-\d{4}\b`, Action: "redact"},
			{ID: "safety-transfer", Kind: "keyword", Pattern: "transfer all funds", Action: "block"},
			{ID: "safety-bomb", Kind: "keyword", Pattern: "how to make a bomb", Action: "block"},
		},
	}
}
