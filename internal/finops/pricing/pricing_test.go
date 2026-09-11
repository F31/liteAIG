package pricing

import (
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/finops/accounting"
)

func testVersion() PriceVersion {
	return PriceVersion{
		ID:          "v1",
		PublishedAt: time.Unix(1, 0),
		Rates: []Rate{
			{Model: "chat", Currency: "USD", InputPerMillion: 3, OutputPerMillion: 15, CacheReadPerMillion: 0.5, CacheWritePerMillion: 3.75},
			{Model: "chat", Currency: "EUR", InputPerMillion: 2.8, OutputPerMillion: 14},
		},
	}
}

func TestPricingGolden(t *testing.T) {
	charge, err := Price(testVersion(), "chat", "USD", 1_000_000, 500_000)
	if err != nil {
		t.Fatal(err)
	}
	// 3 * 1.0 + 15 * 0.5 = 10.5
	if charge != 10.5 {
		t.Fatalf("golden charge = %f, want 10.5", charge)
	}
	if _, err := Price(testVersion(), "unknown", "USD", 1, 1); err != ErrNoRate {
		t.Fatalf("missing rate error = %v", err)
	}
}

// TestPriceCachedGolden verifies cache read/write tokens are priced at their
// declared cache rates while the base input/output keep their rates.
func TestPriceCachedGolden(t *testing.T) {
	charge, err := PriceCached(testVersion(), "chat", "USD", 1_000_000, 500_000, 400_000, 100_000)
	if err != nil {
		t.Fatal(err)
	}
	// input 3*1.0 + output 15*0.5 + cacheRead 0.5*0.4 + cacheWrite 3.75*0.1
	want := 3.0 + 7.5 + 0.2 + 0.375
	if charge != want {
		t.Fatalf("cache-aware charge = %f, want %f", charge, want)
	}
}

// TestPriceCachedFallsBackToBaseInput verifies a rate without declared cache
// pricing prices cached tokens at the base input rate (backward compatible).
func TestPriceCachedFallsBackToBaseInput(t *testing.T) {
	// The EUR rate declares no cache rates: cached tokens price at input rate.
	charge, err := PriceCached(testVersion(), "chat", "EUR", 1_000_000, 0, 1_000_000, 0)
	if err != nil {
		t.Fatal(err)
	}
	// input 2.8*1.0 + cacheRead falls back to 2.8*1.0 = 5.6; no output.
	if charge != 5.6 {
		t.Fatalf("fallback cache charge = %f, want 5.6", charge)
	}
}

// TestPriceCachedZeroCacheMatchesBase ensures calling with zero cache tokens is
// byte-identical to the legacy Price path (no surprise on uncached requests).
func TestPriceCachedZeroCacheMatchesBase(t *testing.T) {
	base, err := Price(testVersion(), "chat", "USD", 100_000, 20_000)
	if err != nil {
		t.Fatal(err)
	}
	cached, err := PriceCached(testVersion(), "chat", "USD", 100_000, 20_000, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if base != cached {
		t.Fatalf("zero-cache cost diverged: base=%f cached=%f", base, cached)
	}
}

func TestDeriveTwoAmountsAndConversion(t *testing.T) {
	cost := 0.5
	facts := accounting.Facts{LogicalModel: "chat", ProviderCurrency: "USD", ProviderCost: &cost, InputTokens: 1_000_000, OutputTokens: 500_000, TenantID: "t", ProjectID: "p"}
	converter := StaticConverter{Rates: map[string]float64{"USD-EUR": 0.9}}
	amounts, err := Derive(testVersion(), facts, "EUR", converter)
	if err != nil {
		t.Fatal(err)
	}
	if amounts.ProviderCost != 0.45 || amounts.CustomerCharge != 9.8 || amounts.PricingVersion != "v1" {
		t.Fatalf("amounts = %+v", amounts)
	}
}

func TestChargebackExcludesUntrustedLabels(t *testing.T) {
	facts := accounting.Facts{TenantID: "t", ProjectID: "p", UserID: "u", OrgUnitID: "org", AttributionTrust: "untrusted"}
	amounts := Amounts{CustomerCharge: 5, PricingVersion: "v1"}
	rows := Chargeback(facts, amounts)
	for _, row := range rows {
		if row.ScopeType == "user" || row.ScopeType == "org_unit" || row.ScopeType == "agent" {
			t.Fatalf("untrusted label produced chargeback row: %+v", row)
		}
	}
	if len(rows) != 2 { // tenant + project only
		t.Fatalf("rows = %+v", rows)
	}

	trusted := facts
	trusted.AttributionTrust = "verified"
	rows = Chargeback(trusted, amounts)
	if len(rows) != 4 {
		t.Fatalf("verified rows = %+v", rows)
	}
}
