package interaction

import "testing"

func TestUnifiedUsageTotalTokens(t *testing.T) {
	usage := UnifiedUsage{InputTokens: 11, OutputTokens: 7}
	if got := usage.TotalTokens(); got != 18 {
		t.Fatalf("TotalTokens() = %d, want 18", got)
	}
}

func TestExternalAgentStaysKindAgentWithBoundary(t *testing.T) {
	context := &Context{
		Kind:          KindAgent,
		TrustBoundary: TrustBoundaryExternalFederated,
		Direction:     DirectionInbound,
		Federation: &FederationContext{
			RelationshipID:  "rel-1",
			ExternalAgentID: "ext-agent",
			AssuranceLevel:  "high",
			DataBoundary:    "contractually_bound",
			TrustAnchorID:   "anchor-1",
		},
	}
	if context.Kind != KindAgent {
		t.Fatalf("external agent must stay Kind=agent, got %s", context.Kind)
	}
	if !context.IsExternalFederated() {
		t.Fatal("external federated boundary not detected")
	}
	if context.Direction != DirectionInbound || context.Federation.RelationshipID != "rel-1" {
		t.Fatalf("context = %+v", context)
	}
	// An internal agent is not federated.
	if (&Context{Kind: KindAgent, TrustBoundary: TrustBoundaryInternal}).IsExternalFederated() {
		t.Fatal("internal agent wrongly federated")
	}
}
