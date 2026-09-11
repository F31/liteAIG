package organization

import (
	"context"
	"time"

	"github.com/F31/liteAIG/internal/tenancy"
)

// Resolver resolves effective-dated attribution for a user.
type Resolver struct {
	repository Repository
	trust      TrustResolver
}

// TrustResolver decides whether a user identity is trusted for attribution.
type TrustResolver func(userID string) string

// NewResolver builds an attribution resolver. trust may be nil, in which case
// resolved attribution is marked as none.
func NewResolver(repository Repository, trust TrustResolver) *Resolver {
	return &Resolver{repository: repository, trust: trust}
}

// Resolve returns the attribution snapshot for a user at a point in time. When no
// effective assignment exists, the user is still attributed with an empty OrgUnit.
func (r *Resolver) Resolve(ctx context.Context, scope tenancy.TenantScope, userID string, at time.Time) PrincipalAttribution {
	trust := "none"
	if r.trust != nil {
		trust = r.trust(userID)
	}
	attribution := PrincipalAttribution{UserID: userID, AttributionTrust: trust}
	assignment, err := r.repository.ActiveAssignment(ctx, scope, userID, at)
	if err != nil || assignment == nil {
		return attribution
	}
	attribution.OrgUnitID = assignment.OrgUnitID
	unit, err := r.repository.GetOrgUnit(ctx, scope, assignment.OrgUnitID)
	if err != nil || unit == nil {
		return attribution
	}
	attribution.OrgPath = unit.Path
	attribution.CostCenterID = unit.CostCenterID
	return attribution
}
