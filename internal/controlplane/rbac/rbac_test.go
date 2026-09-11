package rbac

import (
	"errors"
	"testing"
)

func TestReviewIsAdminOnly(t *testing.T) {
	if err := Require(RoleViewer, PermExternalAgentReview); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer review = %v, want forbidden", err)
	}
	if err := Require(RoleTenantOperator, PermExternalAgentReview); !errors.Is(err, ErrForbidden) {
		t.Fatalf("operator review = %v, want forbidden", err)
	}
	if err := Require(RoleTenantAdmin, PermExternalAgentReview); err != nil {
		t.Fatalf("admin review = %v", err)
	}
	if err := Require(RoleSystemAdmin, PermExternalAgentReview); err != nil {
		t.Fatalf("system admin review = %v", err)
	}
}

func TestSuspendIsOperatorAllowed(t *testing.T) {
	if err := Require(RoleTenantOperator, PermExternalAgentSuspend); err != nil {
		t.Fatalf("operator suspend = %v", err)
	}
	if err := Require(RoleViewer, PermExternalAgentSuspend); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer suspend = %v, want forbidden", err)
	}
}

func TestPermissionsForEnumeratesRoleGrants(t *testing.T) {
	admin := PermissionsFor(RoleTenantAdmin)
	if len(admin) != len(knownPermissions) {
		t.Fatalf("admin permissions = %v", admin)
	}
	systemAdmin := PermissionsFor(RoleSystemAdmin)
	if len(systemAdmin) != len(knownPermissions) {
		t.Fatalf("system admin permissions = %v", systemAdmin)
	}
	operator := PermissionsFor(RoleTenantOperator)
	if len(operator) != 2 || operator[0] != PermExternalAgentSuspend || operator[1] != PermApprovalDecide {
		t.Fatalf("operator permissions = %v", operator)
	}
	viewer := PermissionsFor(RoleViewer)
	if len(viewer) != 0 {
		t.Fatalf("viewer permissions = %v", viewer)
	}
	if len(PermissionsFor("unknown")) != 0 {
		t.Fatal("unknown role has permissions")
	}
}

func TestTrustRotationRequiresPermission(t *testing.T) {
	if err := Require(RoleTenantOperator, PermExternalAgentTrustRotate); !errors.Is(err, ErrForbidden) {
		t.Fatalf("operator trust.rotate = %v, want forbidden", err)
	}
	if err := Require(RoleTenantAdmin, PermExternalAgentTrustRotate); err != nil {
		t.Fatalf("admin trust.rotate = %v", err)
	}
	if err := Require(RoleTenantAdmin, PermDelegationGrant); err != nil {
		t.Fatalf("admin delegation.grant = %v", err)
	}
	if err := Require(RoleTenantAdmin, PermApprovalDecide); err != nil {
		t.Fatalf("admin approval.decide = %v", err)
	}
	if err := Require(RoleViewer, PermApprovalDecide); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer approval.decide = %v, want forbidden", err)
	}
}
