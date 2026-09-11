// Package builtin implements deterministic low-latency guardrail rules.
package builtin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"regexp"
	"slices"
	"strings"
)

type Rule struct{ ID, Kind, Pattern, Action, Replacement string }
type Policy struct {
	Version int64
	Rules   []Rule
}
type Match struct{ RuleID, Action, ContentHash string }
type Result struct {
	Content string
	Blocked bool
	Matches []Match
}
type compiledRule struct {
	Rule
	regex *regexp.Regexp
}

// RuleKinds is the single vocabulary of supported rule kinds. Patterns for
// every kind except keyword are compiled as regular expressions.
var RuleKinds = []string{"keyword", "regex", "secret", "pii", "prompt_injection"}

// regexKinds are the kinds whose Pattern is compiled as a regular expression.
var regexKinds = []string{"regex", "secret", "pii", "prompt_injection"}

type Engine struct {
	version int64
	rules   []compiledRule
}

func New(policy Policy) (*Engine, error) {
	engine := &Engine{version: policy.Version}
	for _, rule := range policy.Rules {
		if rule.ID == "" || rule.Pattern == "" || (rule.Action != "block" && rule.Action != "redact") {
			return nil, errors.New("invalid guardrail rule")
		}
		if !slices.Contains(RuleKinds, rule.Kind) {
			return nil, errors.New("unsupported guardrail rule kind")
		}
		compiled := compiledRule{Rule: rule}
		if slices.Contains(regexKinds, rule.Kind) {
			value, err := regexp.Compile(rule.Pattern)
			if err != nil {
				return nil, err
			}
			compiled.regex = value
		}
		if compiled.Replacement == "" {
			compiled.Replacement = "[redacted]"
		}
		engine.rules = append(engine.rules, compiled)
	}
	return engine, nil
}
func (e *Engine) Evaluate(content string) Result {
	result := Result{Content: content}
	for _, rule := range e.rules {
		matched := false
		switch rule.Kind {
		case "keyword":
			matched = strings.Contains(result.Content, rule.Pattern)
			if matched && rule.Action == "redact" {
				result.Content = strings.ReplaceAll(result.Content, rule.Pattern, rule.Replacement)
			}
		default:
			matched = rule.regex.MatchString(result.Content)
			if matched && rule.Action == "redact" {
				result.Content = rule.regex.ReplaceAllString(result.Content, rule.Replacement)
			}
		}
		if matched {
			sum := sha256.Sum256([]byte(content))
			result.Matches = append(result.Matches, Match{RuleID: rule.ID, Action: rule.Action, ContentHash: hex.EncodeToString(sum[:])})
			if rule.Action == "block" {
				result.Blocked = true
				return result
			}
		}
	}
	return result
}

// Version reports the guardrail policy version compiled into the engine. It
// anchors the reproducible benchmark report's engine version.
func (e *Engine) Version() int64 {
	if e == nil {
		return 0
	}
	return e.version
}

// HasRules reports whether the engine carries any guardrail rules. The
// streaming three-tier guard is only enabled when rules exist, so a default
// empty engine keeps pass-through streaming semantics.
func (e *Engine) HasRules() bool {
	return e != nil && len(e.rules) > 0
}
func (e *Engine) EvaluateMessages(messages []interaction.Message) Result {
	combined := strings.Builder{}
	for _, message := range messages {
		combined.WriteString(message.Role)
		combined.WriteByte('\n')
		combined.WriteString(message.Content)
		combined.WriteByte('\n')
	}
	return e.Evaluate(combined.String())
}
