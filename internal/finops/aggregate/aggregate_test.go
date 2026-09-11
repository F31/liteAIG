package aggregate

import (
	"testing"

	"github.com/F31/liteAIG/internal/finops/accounting"
)

func usage(tenant, project, user, org, agent, task string, tokens, cost int64) accounting.UsageRecord {
	return accounting.UsageRecord{TenantID: tenant, ProjectID: project, UserID: user, OrgUnitID: org, AgentID: agent, TaskID: task, InputTokens: tokens, OutputTokens: tokens, Cost: float64(cost)}
}

func TestByDimensionCrossAnalysis(t *testing.T) {
	records := []accounting.UsageRecord{
		usage("t", "p1", "alice", "eng", "", "", 10, 5),
		usage("t", "p1", "alice", "eng", "", "", 10, 5),
		usage("t", "p2", "bob", "sales", "", "", 20, 9),
	}
	scopes := Scopes()
	projectRows := ByDimension(records, "project", scopes["project"])
	if len(projectRows) != 2 || projectRows[0].ScopeID != "p1" || projectRows[0].Cost != 10 || projectRows[0].Requests != 2 {
		t.Fatalf("project rows = %+v", projectRows)
	}
	userRows := ByDimension(records, "user", scopes["user"])
	if len(userRows) != 2 || userRows[0].ScopeID != "alice" || userRows[0].Cost != 10 {
		t.Fatalf("user rows = %+v", userRows)
	}
}

func TestRootTaskRollsUpHops(t *testing.T) {
	records := []accounting.UsageRecord{
		usage("t", "p", "", "", "", "root", 10, 5),
		usage("t", "p", "", "", "agent-a", "hop-1", 20, 9),
		usage("t", "p", "", "", "agent-b", "hop-2", 5, 2),
		// duplicate hop id is summed once in the total, deduped in hops
		usage("t", "p", "", "", "agent-a", "hop-1", 5, 1),
	}
	total := RootTask(records)
	if total.InputTokens != 40 || total.Cost != 17 || total.Requests != 4 {
		t.Fatalf("root total = %+v", total)
	}
	if len(total.Hops) != 3 {
		t.Fatalf("hops = %+v", total.Hops)
	}
}

func TestByDimensionMatchesLedgerSum(t *testing.T) {
	records := []accounting.UsageRecord{
		usage("t", "p", "", "", "", "", 3, 2),
		usage("t", "p", "", "", "", "", 4, 1),
		usage("t", "p", "", "", "", "", 5, 3),
	}
	rows := ByDimension(records, "project", Scopes()["project"])
	if len(rows) != 1 || rows[0].Cost != 6 || rows[0].InputTokens != 12 || rows[0].Requests != 3 {
		t.Fatalf("aggregation does not match ledger sum: %+v", rows)
	}
}

func TestQuantifyOptimizationCost(t *testing.T) {
	cost := 3.0
	record := accounting.RequestRecord{RetryCount: 2, FallbackCount: 1, ProviderCost: &cost}
	result := QuantifyOptimizationCost(record, 10, 500_000)
	// base = 3/4 = 0.75; retry = 1.5, fallback = 0.75, extra = 2.25
	if result.RetryCost != 1.5 || result.FallbackCost != 0.75 || result.ExtraCost != 2.25 {
		t.Fatalf("optimization cost = %+v", result)
	}
	// cache savings = 0.5M / 1M * 10 = 5
	if result.CacheSavings != 5 {
		t.Fatalf("cache savings = %f", result.CacheSavings)
	}
}
