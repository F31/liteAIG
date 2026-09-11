package tenancy

import (
	"errors"
	"testing"
)

func TestTenantScopeRequiresTenant(t *testing.T) {
	if err := (TenantScope{}).Validate(); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestPageRequestValidation(t *testing.T) {
	if err := (PageRequest{Limit: 1}).Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (PageRequest{Limit: 0}).Validate(); err == nil {
		t.Fatal("Validate() accepted zero limit")
	}
}
