package alert

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestDefaultRulesAreSchemaValid keeps deploy/alerts/default-rules.json a
// contract artifact: every rule must load into the typed model with a known
// rule type/operator/severity, a non-empty metric, and a globally unique id.
func TestDefaultRulesAreSchemaValid(t *testing.T) {
	path := filepath.Join("..", "..", "..", "deploy", "alerts", "default-rules.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read default rules: %v", err)
	}
	var rules []Rule
	if err := json.Unmarshal(data, &rules); err != nil {
		t.Fatalf("default rules are not valid JSON: %v", err)
	}
	if len(rules) == 0 {
		t.Fatal("default ruleset is empty")
	}
	builtIn := DefaultRules()
	if len(rules) != len(builtIn) {
		t.Fatalf("json default rules = %d, built-in = %d", len(rules), len(builtIn))
	}
	knownTypes := map[string]bool{"budget": true, "rate": true, "cost_anomaly": true}
	knownOps := map[string]bool{"gt": true, "gte": true, "lt": true, "lte": true}
	knownSeverity := map[string]bool{SeverityLow: true, SeverityMedium: true, SeverityHigh: true, SeverityCritical: true}
	seen := map[string]bool{}
	for _, rule := range rules {
		if rule.ID == "" || rule.Name == "" || rule.Metric == "" {
			t.Fatalf("rule with empty id/name/metric: %+v", rule)
		}
		if seen[rule.ID] {
			t.Fatalf("duplicate default rule id %q", rule.ID)
		}
		seen[rule.ID] = true
		if !knownTypes[rule.RuleType] {
			t.Fatalf("rule %s has unknown ruleType %q", rule.ID, rule.RuleType)
		}
		if !knownOps[rule.Operator] {
			t.Fatalf("rule %s has unknown operator %q", rule.ID, rule.Operator)
		}
		if !knownSeverity[rule.Severity] {
			t.Fatalf("rule %s has unknown severity %q", rule.ID, rule.Severity)
		}
		if rule.WindowSeconds <= 0 || rule.Threshold == 0 {
			t.Fatalf("rule %s has non-positive window/threshold: %+v", rule.ID, rule)
		}
	}
	for i, rule := range rules {
		want := builtIn[i]
		if rule.ID != want.ID || rule.Name != want.Name || rule.RuleType != want.RuleType || rule.Metric != want.Metric || rule.Operator != want.Operator || rule.Threshold != want.Threshold || rule.WindowSeconds != want.WindowSeconds || rule.Severity != want.Severity || rule.Enabled != want.Enabled {
			t.Fatalf("json rule[%d]=%+v, built-in=%+v", i, rule, want)
		}
	}
}
