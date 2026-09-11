package golden

import (
	"math"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/finops/accounting"
	"github.com/F31/liteAIG/internal/finops/aggregate"
	"github.com/F31/liteAIG/internal/finops/pricing"
)

func TestScenarioCEnterpriseFinOpsAttribution(t *testing.T) {
	version := pricing.PriceVersion{
		ID: "v1", PublishedAt: time.Unix(1, 0),
		Rates: []pricing.Rate{{Model: "chat", Currency: "EUR", InputPerMillion: 2.8, OutputPerMillion: 14}},
	}
	converter := pricing.StaticConverter{Rates: map[string]float64{"USD-EUR": 0.9}}

	// Build usage facts across 2 departments, 2 projects, with a reassignment
	// that must NOT rewrite historical attribution.
	facts := []accounting.Facts{
		{TenantID: "t", ProjectID: "p1", LogicalModel: "chat", InputTokens: 1_000_000, OutputTokens: 500_000, UserID: "alice", OrgUnitID: "eng", AttributionTrust: "verified"},
		{TenantID: "t", ProjectID: "p1", LogicalModel: "chat", InputTokens: 1_000_000, OutputTokens: 500_000, UserID: "alice", OrgUnitID: "eng", AttributionTrust: "verified"},
		{TenantID: "t", ProjectID: "p2", LogicalModel: "chat", InputTokens: 1_000_000, OutputTokens: 500_000, UserID: "bob", OrgUnitID: "sales", AttributionTrust: "verified"},
		{TenantID: "t", ProjectID: "p2", LogicalModel: "chat", InputTokens: 100_000, OutputTokens: 0, UserID: "mallory", OrgUnitID: "eng", AttributionTrust: "untrusted"},
	}

	var records []accounting.UsageRecord
	var chargeableRows []pricing.ChargebackLine
	for _, fact := range facts {
		amounts, err := pricing.Derive(version, fact, "EUR", converter)
		if err != nil {
			t.Fatal(err)
		}
		// Provider cost from the version since facts carry none.
		records = append(records, accounting.UsageRecord{TenantID: fact.TenantID, ProjectID: fact.ProjectID, UserID: fact.UserID, OrgUnitID: fact.OrgUnitID, AttributionTrust: fact.AttributionTrust, InputTokens: fact.InputTokens, OutputTokens: fact.OutputTokens, Cost: amounts.CustomerCharge})
		chargeableRows = append(chargeableRows, pricing.Chargeback(fact, amounts)...)
	}

	// Project x Department cross analysis reconciles to the ledger.
	projectRows := aggregate.ByDimension(records, "project", aggregate.Scopes()["project"])
	engRows := aggregate.ByDimension(records, "org_unit", aggregate.Scopes()["org_unit"])
	if len(projectRows) != 2 || len(engRows) != 2 {
		t.Fatalf("project=%+v eng=%+v", projectRows, engRows)
	}
	// alice (2 verified calls at 9.8 each) + mallory untrusted eng call: the
	// aggregation shows all tokens (trust is a displayed dimension), so eng
	// cost = 19.6 + 0.28 = 19.88.
	var engCost float64
	for _, row := range engRows {
		if row.ScopeID == "eng" {
			engCost = row.Cost
		}
	}
	if !approx(engCost, 19.88) {
		t.Fatalf("eng aggregate cost = %f, want 19.88", engCost)
	}

	// Untrusted labels must not produce User/OrgUnit chargeback rows.
	for _, row := range chargeableRows {
		if row.ScopeID == "mallory" || row.ScopeType == "org_unit" && row.ScopeID == "eng" && row.CustomerCharge > 19.6 {
			t.Fatalf("untrusted chargeback row: %+v", row)
		}
	}

	// Ledger reconciliation: tenant-level chargeback covers every fact,
	// including untrusted ones billed at Tenant/Project/Key scope.
	var chargebackTotal float64
	for _, row := range chargeableRows {
		if row.ScopeType == "tenant" {
			chargebackTotal += row.CustomerCharge
		}
	}
	if !approx(chargebackTotal, 29.68) { // alice 19.6 + bob 9.8 + mallory 0.28
		t.Fatalf("chargeback total = %f", chargebackTotal)
	}
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
