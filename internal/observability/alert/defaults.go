package alert

// DefaultRules returns the built-in alert rules operators can import into a
// tenant. The deploy/alerts/default-rules.json contract mirrors this list.
func DefaultRules() []Rule {
	return []Rule{
		{ID: "dr-budget-soft", Name: "Soft budget threshold crossed", RuleType: "budget", Metric: "budget.usage", Operator: "gte", Threshold: 0.8, WindowSeconds: 3600, Severity: SeverityMedium, Enabled: true},
		{ID: "dr-budget-hard", Name: "Hard budget threshold crossed", RuleType: "budget", Metric: "budget.usage", Operator: "gte", Threshold: 1.0, WindowSeconds: 3600, Severity: SeverityHigh, Enabled: true},
		{ID: "dr-request-spike", Name: "Request rate spike", RuleType: "rate", Metric: "rate.requests_per_minute", Operator: "gte", Threshold: 2000, WindowSeconds: 300, Severity: SeverityLow, Enabled: true},
		{ID: "dr-cost-anomaly", Name: "Provider cost anomaly", RuleType: "cost_anomaly", Metric: "cost.anomaly_score", Operator: "gte", Threshold: 3.0, WindowSeconds: 3600, Severity: SeverityHigh, Enabled: true},
	}
}
