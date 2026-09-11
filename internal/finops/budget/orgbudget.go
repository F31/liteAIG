package budget

import (
	"time"

	"github.com/F31/liteAIG/internal/identity"
	"github.com/F31/liteAIG/internal/organization"
	"github.com/F31/liteAIG/internal/platform/coordination"
)

// OrgBudgetGate adds User/OrgUnit budget windows only when attribution is trusted.
type OrgBudgetGate struct {
	// TenantTag is the Redis hash tag for this tenant's keys, e.g. "{tenant:<id>}".
	TenantTag string
}

// UserOrgWindowKeys returns stable budget window keys for a trusted user.
func (g OrgBudgetGate) UserOrgWindowKeys(attribution organization.PrincipalAttribution, windowHours int64) ([]coordination.BudgetWindow, bool) {
	if !identity.TrustedAttribution(attribution.AttributionTrust) {
		return nil, false
	}
	var windows []coordination.BudgetWindow
	if attribution.UserID != "" {
		windows = append(windows, coordination.BudgetWindow{
			Key: "budget:" + g.TenantTag + ":user:" + attribution.UserID + ":1d",
		})
	}
	if attribution.OrgUnitID != "" {
		windows = append(windows, coordination.BudgetWindow{
			Key: "budget:" + g.TenantTag + ":org:" + attribution.OrgUnitID + ":1d",
		})
	}
	// Apply window hours as the window counter TTL so stale counters expire.
	for i := range windows {
		if windowHours > 0 {
			windows[i].TTL = time.Duration(windowHours) * time.Hour
		}
	}
	return windows, len(windows) > 0
}

// Apply appends the User/OrgUnit windows to a reserve request when trusted.
func (g OrgBudgetGate) Apply(request coordination.ReserveRequest, attribution organization.PrincipalAttribution, windowHours int64) coordination.ReserveRequest {
	windows, ok := g.UserOrgWindowKeys(attribution, windowHours)
	if !ok {
		return request
	}
	request.Windows = append(request.Windows, windows...)
	return request
}
