// Package federation owns cross-organization Agent trust.
package federation

// EffectiveFederatedPermission is the local intersection of every factor
// LiteAIG can prove. It never assumes knowledge of the partner's internal
// policy; any absent or failing factor denies the call.
type EffectiveFederatedPermission struct {
	SystemOK          bool
	TenantOK          bool
	ProjectOK         bool
	CallerOK          bool
	LocalDelegationOK bool
	RelationshipOK    bool
	ProjectGrantOK    bool
	CapabilityGrantOK bool
	DataBoundaryOK    bool
}

// Allowed reports whether every factor passes.
func (p EffectiveFederatedPermission) Allowed() bool {
	return p.SystemOK && p.TenantOK && p.ProjectOK && p.CallerOK &&
		p.LocalDelegationOK && p.RelationshipOK && p.ProjectGrantOK &&
		p.CapabilityGrantOK && p.DataBoundaryOK
}

// Request carries the facts needed to evaluate federated permission.
type PermissionRequest struct {
	SystemPolicyEnabled    bool
	TenantActive           bool
	ProjectActive          bool
	CallerAllowed          bool
	LocalDelegationGranted bool
	RelationshipActive     bool
	ProjectGranted         bool
	CapabilityGranted      bool
	DataBoundarySatisfied  bool
}

// Evaluate computes the intersection for a request.
func Evaluate(request PermissionRequest) EffectiveFederatedPermission {
	return EffectiveFederatedPermission{
		SystemOK:          request.SystemPolicyEnabled,
		TenantOK:          request.TenantActive,
		ProjectOK:         request.ProjectActive,
		CallerOK:          request.CallerAllowed,
		LocalDelegationOK: request.LocalDelegationGranted,
		RelationshipOK:    request.RelationshipActive,
		ProjectGrantOK:    request.ProjectGranted,
		CapabilityGrantOK: request.CapabilityGranted,
		DataBoundaryOK:    request.DataBoundarySatisfied,
	}
}
