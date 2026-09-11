// Package aggregate derives multi-dimensional FinOps rollups from Usage Ledger
// facts so Dashboard totals and ledger SQL reconcile to the same numbers, and
// rolls child-task / agent-hop usage up to the Root Task.
package aggregate

import (
	"time"

	"github.com/F31/liteAIG/internal/finops/accounting"
)

// Row is one aggregated dimension group.
type Row struct {
	ScopeType    string
	ScopeID      string
	TenantID     string
	ProjectID    string
	InputTokens  int64
	OutputTokens int64
	Cost         float64
	Requests     int64
}

// Selector extracts a scope id from a usage record.
type Selector func(accounting.UsageRecord) (id string, present bool)

// Scopes returns selectors for common dimensions.
func Scopes() map[string]Selector {
	return map[string]Selector{
		"tenant":   func(r accounting.UsageRecord) (string, bool) { return r.TenantID, r.TenantID != "" },
		"project":  func(r accounting.UsageRecord) (string, bool) { return r.ProjectID, r.ProjectID != "" },
		"user":     func(r accounting.UsageRecord) (string, bool) { return r.UserID, r.UserID != "" },
		"org_unit": func(r accounting.UsageRecord) (string, bool) { return r.OrgUnitID, r.OrgUnitID != "" },
		"agent":    func(r accounting.UsageRecord) (string, bool) { return r.AgentID, r.AgentID != "" },
		"task":     func(r accounting.UsageRecord) (string, bool) { return r.TaskID, r.TaskID != "" },
	}
}

// ByDimension groups usage records by one scope.
func ByDimension(records []accounting.UsageRecord, scopeType string, selectID Selector) []Row {
	index := map[string]*Row{}
	var order []string
	for _, record := range records {
		id, present := selectID(record)
		if !present {
			continue
		}
		row, ok := index[id]
		if !ok {
			row = &Row{ScopeType: scopeType, ScopeID: id, TenantID: record.TenantID, ProjectID: record.ProjectID}
			index[id] = row
			order = append(order, id)
		}
		row.InputTokens += record.InputTokens
		row.OutputTokens += record.OutputTokens
		row.Cost += record.Cost
		row.Requests++
	}
	result := make([]Row, 0, len(order))
	for _, id := range order {
		result = append(result, *index[id])
	}
	return result
}

// Hop is one task/hop usage contribution.
type Hop struct {
	TaskID       string
	AgentID      string
	Cost         float64
	InputTokens  int64
	OutputTokens int64
}

// RootTaskTotal is the aggregated result for one root task.
type RootTaskTotal struct {
	InputTokens, OutputTokens, Requests int64
	Cost                                float64
	Hops                                []Hop
}

// RootTask totals a root task's own usage plus all descendant task/agent-hop
// usage, preserving a per-hop breakdown.
func RootTask(records []accounting.UsageRecord) RootTaskTotal {
	total := RootTaskTotal{Hops: []Hop{}}
	seen := map[string]bool{}
	for _, record := range records {
		total.InputTokens += record.InputTokens
		total.OutputTokens += record.OutputTokens
		total.Cost += record.Cost
		total.Requests++
		hopID := record.TaskID
		if hopID == "" {
			hopID = record.RequestID
		}
		if seen[hopID] {
			continue
		}
		seen[hopID] = true
		total.Hops = append(total.Hops, Hop{TaskID: record.TaskID, AgentID: record.AgentID, Cost: record.Cost, InputTokens: record.InputTokens, OutputTokens: record.OutputTokens})
	}
	return total
}

// Now is exposed for determinism in tests.
func Now() time.Time { return time.Now().UTC() }

// OptimizationCost estimates the provider cost attributable to retries and
// fallbacks for a request record, and derives cash-equivalent cache savings
// from saved tokens and a per-million-token charge.
type OptimizationCost struct {
	RetryCost    float64
	FallbackCost float64
	ExtraCost    float64
	CacheSavings float64
}

// QuantifyOptimizationCost estimates retry/fallback extra cost from the
// request's attempt counts and provider cost, plus cache savings from saved
// tokens priced at chargePerMillion.
func QuantifyOptimizationCost(record accounting.RequestRecord, chargePerMillion float64, cacheSavedTokens int64) OptimizationCost {
	result := OptimizationCost{}
	if record.ProviderCost != nil {
		total := record.RetryCount + record.FallbackCount
		attempts := total + 1
		if attempts > 1 {
			base := *record.ProviderCost / float64(attempts)
			result.RetryCost = base * float64(record.RetryCount)
			result.FallbackCost = base * float64(record.FallbackCount)
			result.ExtraCost = base * float64(total)
		}
	}
	if chargePerMillion > 0 && cacheSavedTokens > 0 {
		result.CacheSavings = float64(cacheSavedTokens) / 1_000_000 * chargePerMillion
	}
	return result
}
