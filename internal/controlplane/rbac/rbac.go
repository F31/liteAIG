// Package rbac provides the backend-authoritative role→permission matrix for
// federated agent, approval, and delegation actions. The frontend is
// display-only; enforcement always happens here.
package rbac

import "errors"

// Roles.
const (
	RoleSystemAdmin    = "system_admin"
	RoleTenantAdmin    = "tenant_admin"
	RoleTenantOperator = "tenant_operator"
	RoleViewer         = "viewer"
)

// Permissions.
const (
	PermExternalAgentReview      = "external_agent.review"
	PermExternalAgentSuspend     = "external_agent.suspend"
	PermExternalAgentTrustRotate = "external_agent.trust.rotate"
	PermExternalAgentTrustRevoke = "external_agent.trust.revoke"
	PermApprovalDecide           = "approval.decide"
	PermDelegationGrant          = "delegation.grant"
	PermDelegationRevoke         = "delegation.revoke"
	PermEvidenceExport           = "evidence.export"
)

// ErrForbidden reports a permission denial.
var ErrForbidden = errors.New("forbidden: role lacks permission")

// matrix maps each role to its granted permissions.
var matrix = map[string]map[string]bool{
	RoleSystemAdmin: {
		PermExternalAgentReview:      true,
		PermExternalAgentSuspend:     true,
		PermExternalAgentTrustRotate: true,
		PermExternalAgentTrustRevoke: true,
		PermApprovalDecide:           true,
		PermDelegationGrant:          true,
		PermDelegationRevoke:         true,
		PermEvidenceExport:           true,
	},
	RoleTenantAdmin: {
		PermExternalAgentReview:      true,
		PermExternalAgentSuspend:     true,
		PermExternalAgentTrustRotate: true,
		PermExternalAgentTrustRevoke: true,
		PermApprovalDecide:           true,
		PermDelegationGrant:          true,
		PermDelegationRevoke:         true,
		PermEvidenceExport:           true,
	},
	RoleTenantOperator: {
		PermExternalAgentSuspend: true,
		PermApprovalDecide:       true,
	},
	RoleViewer: {},
}

// Roles returns the known role names for validation.
func Roles() []string {
	return []string{RoleSystemAdmin, RoleTenantAdmin, RoleTenantOperator, RoleViewer}
}

// TenantRoles returns roles assignable inside a tenant-local user directory.
func TenantRoles() []string { return []string{RoleTenantAdmin, RoleTenantOperator, RoleViewer} }

// knownPermissions lists every permission in a stable order for enumeration.
var knownPermissions = []string{
	PermExternalAgentReview,
	PermExternalAgentSuspend,
	PermExternalAgentTrustRotate,
	PermExternalAgentTrustRevoke,
	PermApprovalDecide,
	PermDelegationGrant,
	PermDelegationRevoke,
	PermEvidenceExport,
}

// PermissionsFor returns the permissions granted to a role, in stable order.
func PermissionsFor(role string) []string {
	result := make([]string, 0, len(knownPermissions))
	for _, permission := range knownPermissions {
		if Has(role, permission) {
			result = append(result, permission)
		}
	}
	return result
}

// Has reports whether a role holds a permission.
func Has(role, permission string) bool {
	permissions, ok := matrix[role]
	if !ok {
		return false
	}
	return permissions[permission]
}

// Require enforces a permission; it returns ErrForbidden when denied.
func Require(role, permission string) error {
	if !Has(role, permission) {
		return ErrForbidden
	}
	return nil
}
