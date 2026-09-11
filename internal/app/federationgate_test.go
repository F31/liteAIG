package app

import (
	"testing"

	"github.com/F31/liteAIG/internal/kernel/runtime"
)

func TestDataBoundarySatisfied(t *testing.T) {
	cases := []struct {
		name    string
		project runtime.Project
		rel     runtime.FederationRelationship
		want    bool
	}{
		{"disjoint declared regions denied", runtime.Project{AllowedDataRegions: []string{"us-east-1"}}, runtime.FederationRelationship{ProcessingRegions: []string{"eu-central-1"}}, false},
		{"overlapping declared regions allowed", runtime.Project{AllowedDataRegions: []string{"us-east-1", "eu-central-1"}}, runtime.FederationRelationship{ProcessingRegions: []string{"eu-central-1"}}, true},
		{"relationship declares no regions allowed", runtime.Project{AllowedDataRegions: []string{"us-east-1"}}, runtime.FederationRelationship{ProcessingRegions: nil}, true},
		{"project allows no regions allowed", runtime.Project{AllowedDataRegions: nil}, runtime.FederationRelationship{ProcessingRegions: []string{"eu-central-1"}}, true},
		{"neither side declares regions allowed", runtime.Project{}, runtime.FederationRelationship{}, true},
		{"empty project regions with declared relationship regions allowed", runtime.Project{}, runtime.FederationRelationship{ProcessingRegions: []string{"eu-central-1"}}, true},
		{"unexpected boundary status fails closed", runtime.Project{}, runtime.FederationRelationship{BoundaryStatus: "not-a-real-status"}, false},
		{"unknown boundary status allowed", runtime.Project{}, runtime.FederationRelationship{BoundaryStatus: "unknown"}, true},
		{"declared boundary status allowed", runtime.Project{}, runtime.FederationRelationship{BoundaryStatus: "declared"}, true},
		{"contractually bound status allowed", runtime.Project{}, runtime.FederationRelationship{BoundaryStatus: "contractually_bound"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dataBoundarySatisfied(tc.project, tc.rel); got != tc.want {
				t.Fatalf("dataBoundarySatisfied(%+v, %+v) = %t, want %t", tc.project, tc.rel, got, tc.want)
			}
		})
	}
}

func TestDirectionAllowsOutbound(t *testing.T) {
	for _, allowed := range []string{"", "outbound", "bidirectional"} {
		if !directionAllowsOutbound(allowed) {
			t.Fatalf("directionAllowsOutbound(%q) = false, want true", allowed)
		}
	}
	for _, denied := range []string{"inbound", "weird", "reverse"} {
		if directionAllowsOutbound(denied) {
			t.Fatalf("directionAllowsOutbound(%q) = true, want false", denied)
		}
	}
}

func TestApprovedVersionMatches(t *testing.T) {
	cases := []struct {
		name            string
		approved        string
		endpointVersion string
		want            bool
	}{
		{"unpinned approved version allows", "", "1", true},
		{"endpoint without version allows", "2", "", true},
		{"matching version allows", "1", "1", true},
		{"mismatched version denies", "2", "1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := approvedVersionMatches(tc.approved, tc.endpointVersion); got != tc.want {
				t.Fatalf("approvedVersionMatches(%q, %q) = %t, want %t", tc.approved, tc.endpointVersion, got, tc.want)
			}
		})
	}
}

func TestProjectAndCapabilityGranted(t *testing.T) {
	rel := runtime.FederationRelationship{ProjectGrants: []string{"p1"}, CapabilityGrants: []string{"chat"}}
	if !projectGranted(rel, "p1") || projectGranted(rel, "p2") {
		t.Fatalf("projectGranted(%+v) mis-evaluated project membership", rel)
	}
	if !capabilityGranted(rel, "chat") || capabilityGranted(rel, "read") {
		t.Fatalf("capabilityGranted(%+v) mis-evaluated capability membership", rel)
	}
	empty := runtime.FederationRelationship{}
	if projectGranted(empty, "p1") || capabilityGranted(empty, "chat") {
		t.Fatalf("empty grants must deny every project/capability")
	}
}
