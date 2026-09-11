package identity

import (
	"reflect"
	"testing"
)

func TestDelegationCannotWiden(t *testing.T) {
	delegator := []string{"invoice.read", "invoice.pay"}
	grant := []string{"invoice.read"} // narrower grant
	policy := []string{"invoice.read", "invoice.pay"}
	result, err := EvaluateDelegation(delegator, grant, policy)
	if err != nil {
		t.Fatal(err)
	}
	// Effective permission is the intersection: only invoice.read.
	if !reflect.DeepEqual(result, []string{"invoice.read"}) {
		t.Fatalf("delegation result = %v, want [invoice.read]", result)
	}
}

func TestDelegationDelegateePolicyBinds(t *testing.T) {
	delegator := []string{"invoice.read", "payment.execute"}
	grant := []string{"invoice.read", "payment.execute"}
	policy := []string{"invoice.read"} // delegatee policy is stricter
	result, err := EvaluateDelegation(delegator, grant, policy)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result, []string{"invoice.read"}) {
		t.Fatalf("delegatee policy not enforced: %v", result)
	}
}

func TestAgentACLGatesDelegation(t *testing.T) {
	acl := AgentACL{TenantID: "tenant", Allowed: map[string]bool{"agent-b": true}}
	if !acl.Allows("agent-b") {
		t.Fatal("allowed delegatee denied")
	}
	if acl.Allows("agent-c") {
		t.Fatal("non-ACL delegatee allowed")
	}
}
