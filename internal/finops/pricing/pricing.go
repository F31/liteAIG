// Package pricing owns Pricing Versions, two-amount derivation (Provider Cost
// vs Customer Charge), multi-currency conversion, and attribution-qualified
// chargeback/showback derivation.
package pricing

import (
	"errors"
	"time"

	"github.com/F31/liteAIG/internal/finops/accounting"
)

var ErrNoRate = errors.New("no price rate for model/currency")

// Rate prices one model in one currency per million tokens.
type Rate struct {
	Model            string
	Currency         string
	InputPerMillion  float64
	OutputPerMillion float64
	// CacheReadPerMillion prices prompt-cache hit tokens (Anthropic's
	// cache_read_input_tokens / OpenAI's discounted cached input). Zero falls
	// back to InputPerMillion for providers that report cached tokens without a
	// separate published rate.
	CacheReadPerMillion float64
	// CacheWritePerMillion prices the first-time write an explicit cache
	// marker triggers on cache_miss (Anthropic cache_creation_input_tokens).
	// Zero falls back to InputPerMillion.
	CacheWritePerMillion float64
}

// PriceVersion is an immutable published price list.
type PriceVersion struct {
	ID          string
	PublishedAt time.Time
	Rates       []Rate
}

// LiteReferencePriceVersion is the versioned built-in USD estimate used when
// provider billing facts are unavailable. It is reference pricing, not invoice
// reconciliation; callers must leave unknown models unpriced.
var LiteReferencePriceVersion = PriceVersion{
	ID: "lite-reference-v1",
	Rates: []Rate{
		{Model: "gpt-4o-mini", Currency: "USD", InputPerMillion: 0.15, OutputPerMillion: 0.60, CacheReadPerMillion: 0.075},
		{Model: "gpt-4o", Currency: "USD", InputPerMillion: 2.50, OutputPerMillion: 10.00, CacheReadPerMillion: 1.25},
		{Model: "gpt-4.1", Currency: "USD", InputPerMillion: 2.00, OutputPerMillion: 8.00, CacheReadPerMillion: 0.50},
		{Model: "o1-mini", Currency: "USD", InputPerMillion: 1.10, OutputPerMillion: 4.40, CacheReadPerMillion: 0.55},
		{Model: "claude-3-5-sonnet", Currency: "USD", InputPerMillion: 3.00, OutputPerMillion: 15.00, CacheReadPerMillion: 0.30, CacheWritePerMillion: 3.75},
		{Model: "claude-3-5-haiku", Currency: "USD", InputPerMillion: 0.80, OutputPerMillion: 4.00, CacheReadPerMillion: 0.08, CacheWritePerMillion: 1.00},
	},
}

func (v PriceVersion) rate(model, currency string) (Rate, bool) {
	for _, rate := range v.Rates {
		if rate.Model == model && rate.Currency == currency {
			return rate, true
		}
	}
	return Rate{}, false
}

// Price computes the customer charge for a usage fact under a version.
func Price(version PriceVersion, model, currency string, inputTokens, outputTokens int64) (float64, error) {
	return PriceCached(version, model, currency, inputTokens, outputTokens, 0, 0)
}

// PriceCached computes the customer charge for a usage fact under a version,
// pricing cached input at the cache-read rate and cache-creation tokens at the
// cache-write rate (both falling back to the base input rate). This is how
// provider prompt-cache hits translate into real costs (§11.3).
func PriceCached(version PriceVersion, model, currency string, inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens int64) (float64, error) {
	rate, ok := version.rate(model, currency)
	if !ok {
		return 0, ErrNoRate
	}
	cacheRead, cacheWrite := rate.CacheReadPerMillion, rate.CacheWritePerMillion
	if cacheRead <= 0 {
		cacheRead = rate.InputPerMillion
	}
	if cacheWrite <= 0 {
		cacheWrite = rate.InputPerMillion
	}
	input := float64(inputTokens) / 1_000_000 * rate.InputPerMillion
	output := float64(outputTokens) / 1_000_000 * rate.OutputPerMillion
	read := float64(cacheReadTokens) / 1_000_000 * cacheRead
	write := float64(cacheWriteTokens) / 1_000_000 * cacheWrite
	return input + output + read + write, nil
}

// Amounts carries the two amount sets derived for a usage fact.
type Amounts struct {
	ProviderCost   float64
	CustomerCharge float64
	Currency       string
	PricingVersion string
}

// Converter converts an amount from a source currency to a settlement currency.
type Converter interface {
	Convert(amount float64, from, to string) (float64, error)
}

// StaticConverter uses a fixed rate map (test/default) with validation.
type StaticConverter struct{ Rates map[string]float64 }

func (c StaticConverter) Convert(amount float64, from, to string) (float64, error) {
	if from == to {
		return amount, nil
	}
	if c.Rates == nil {
		return 0, errors.New("no conversion rate source")
	}
	rate, ok := c.Rates[from+"-"+to]
	if !ok {
		return 0, errors.New("missing conversion rate " + from + " -> " + to)
	}
	return amount * rate, nil
}

// Derive produces the two amount sets for a usage fact. Provider cost is taken
// from the fact when present, else computed from the version; the customer
// charge is always computed from the version, then both are converted to the
// tenant settlement currency. Cache read/write tokens are priced at their
// cache rates when the version declares them.
func Derive(version PriceVersion, facts accounting.Facts, settlementCurrency string, converter Converter) (Amounts, error) {
	currency := facts.ProviderCurrency
	if currency == "" {
		currency = settlementCurrency
	}
	providerCost := 0.0
	if facts.ProviderCost != nil {
		providerCost = *facts.ProviderCost
	} else {
		var err error
		providerCost, err = PriceCached(version, facts.LogicalModel, currency, facts.InputTokens, facts.OutputTokens, facts.CacheReadTokens, facts.CacheWriteTokens)
		if err != nil {
			return Amounts{}, err
		}
	}
	customerCharge, err := PriceCached(version, facts.LogicalModel, settlementCurrency, facts.InputTokens, facts.OutputTokens, facts.CacheReadTokens, facts.CacheWriteTokens)
	if err != nil {
		return Amounts{}, err
	}
	if converter != nil {
		if providerCost, err = converter.Convert(providerCost, currency, settlementCurrency); err != nil {
			return Amounts{}, err
		}
	}
	return Amounts{
		ProviderCost:   providerCost,
		CustomerCharge: customerCharge,
		Currency:       settlementCurrency,
		PricingVersion: version.ID,
	}, nil
}

// Chargeable reports whether a fact qualifies for User/OrgUnit/Agent chargeback.
func Chargeable(facts accounting.Facts) bool {
	switch facts.AttributionTrust {
	case "verified", "key_bound", "delegated":
		return true
	default:
		return false
	}
}

// ChargebackLine is one attribution-qualified chargeback row.
type ChargebackLine struct {
	TenantID, ProjectID, ScopeType, ScopeID string
	CustomerCharge                          float64
	PricingVersion                          string
}

// Chargeback derives tenant/project-level rows plus attribution-qualified
// User/OrgUnit/Agent rows. Untrusted labels are never used for User/OrgUnit/
// Agent chargeback.
func Chargeback(facts accounting.Facts, amounts Amounts) []ChargebackLine {
	rows := []ChargebackLine{
		{TenantID: facts.TenantID, ProjectID: facts.ProjectID, ScopeType: "tenant", ScopeID: facts.TenantID, CustomerCharge: amounts.CustomerCharge, PricingVersion: amounts.PricingVersion},
		{TenantID: facts.TenantID, ProjectID: facts.ProjectID, ScopeType: "project", ScopeID: facts.ProjectID, CustomerCharge: amounts.CustomerCharge, PricingVersion: amounts.PricingVersion},
	}
	if !Chargeable(facts) {
		return rows
	}
	if facts.UserID != "" {
		rows = append(rows, ChargebackLine{TenantID: facts.TenantID, ProjectID: facts.ProjectID, ScopeType: "user", ScopeID: facts.UserID, CustomerCharge: amounts.CustomerCharge, PricingVersion: amounts.PricingVersion})
	}
	if facts.OrgUnitID != "" {
		rows = append(rows, ChargebackLine{TenantID: facts.TenantID, ProjectID: facts.ProjectID, ScopeType: "org_unit", ScopeID: facts.OrgUnitID, CustomerCharge: amounts.CustomerCharge, PricingVersion: amounts.PricingVersion})
	}
	if facts.AgentID != "" {
		rows = append(rows, ChargebackLine{TenantID: facts.TenantID, ProjectID: facts.ProjectID, ScopeType: "agent", ScopeID: facts.AgentID, CustomerCharge: amounts.CustomerCharge, PricingVersion: amounts.PricingVersion})
	}
	return rows
}
