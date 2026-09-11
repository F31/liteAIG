// Package identity owns the canonical principal model and delegation grants.
package identity

import (
	"errors"
	"time"
)

// DelegationGrant authorizes one agent to delegate to another, with a bounded
// permission set. Delegation never widens privilege.
type DelegationGrant struct {
	ID          string
	TenantID    string
	DelegatorID string
	DelegateeID string
	Permissions []string
	CreatedBy   string
	CreatedAt   time.Time
}

// AgentACL enumerates which agents may receive a delegation.
type AgentACL struct {
	TenantID string
	Allowed  map[string]bool
}

// Allows reports whether a delegatee is in the ACL.
func (a AgentACL) Allows(delegateeID string) bool {
	return a.Allowed[delegateeID]
}

// EvaluateDelegation computes the intersection of delegator permission, grant
// permission, and delegatee policy. The result can only be equal to or smaller
// than the delegator's permission set.
func EvaluateDelegation(delegatorPermissions, grantPermissions, delegateePolicy []string) ([]string, error) {
	result := []string{}
	grant := toSet(grantPermissions)
	policy := toSet(delegateePolicy)
	for _, permission := range delegatorPermissions {
		if grant[permission] && policy[permission] {
			result = append(result, permission)
		}
	}
	if len(result) > len(delegatorPermissions) {
		return nil, errors.New("delegation widened privilege")
	}
	return result, nil
}

func toSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}
