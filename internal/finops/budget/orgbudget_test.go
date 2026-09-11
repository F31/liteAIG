package budget

import (
	"testing"

	"github.com/F31/liteAIG/internal/organization"
	"github.com/F31/liteAIG/internal/platform/coordination"
)

func TestUntrustedAttributionAddsNoUserOrgWindows(t *testing.T) {
	gate := OrgBudgetGate{TenantTag: "{tenant:t}"}
	attribution := organization.PrincipalAttribution{UserID: "u", OrgUnitID: "o", AttributionTrust: "untrusted"}
	request := coordination.ReserveRequest{Windows: []coordination.BudgetWindow{{Key: "tenant-window"}}}
	result := gate.Apply(request, attribution, 24)
	if len(result.Windows) != 1 {
		t.Fatalf("windows = %v", result.Windows)
	}
}

func TestTrustedAttributionAddsUserAndOrgWindows(t *testing.T) {
	gate := OrgBudgetGate{TenantTag: "{tenant:t}"}
	attribution := organization.PrincipalAttribution{UserID: "u", OrgUnitID: "o", AttributionTrust: "verified"}
	windows, ok := gate.UserOrgWindowKeys(attribution, 24)
	if !ok {
		t.Fatal("trusted attribution produced no windows")
	}
	if len(windows) != 2 {
		t.Fatalf("windows = %v", windows)
	}
	for _, window := range windows {
		if window.TTL == 0 {
			t.Fatalf("window %s missing TTL", window.Key)
		}
	}
}

func TestDelegatedAttributionIsTrusted(t *testing.T) {
	gate := OrgBudgetGate{TenantTag: "{tenant:t}"}
	windows, ok := gate.UserOrgWindowKeys(organization.PrincipalAttribution{UserID: "u", AttributionTrust: "delegated"}, 24)
	if !ok || len(windows) != 1 {
		t.Fatalf("windows = %v, ok = %t", windows, ok)
	}
}

func TestKeyBoundAttributionIsNotTrustedForUserOrg(t *testing.T) {
	gate := OrgBudgetGate{TenantTag: "{tenant:t}"}
	_, ok := gate.UserOrgWindowKeys(organization.PrincipalAttribution{UserID: "u", AttributionTrust: "key_bound"}, 24)
	if ok {
		t.Fatal("key_bound attribution must not enable User/OrgUnit windows")
	}
}
