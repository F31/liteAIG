package scim

import (
	"context"
	"errors"
	"testing"
)

func TestMockProvisionerUpsertAndDeactivate(t *testing.T) {
	ctx := context.Background()
	provisioner := NewMockProvisioner()
	user := User{ExternalID: "ext-1", Email: "a@example.com", DisplayName: "Alice", Active: true, Groups: []string{"eng"}}
	if err := provisioner.UpsertUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	updated := User{ExternalID: "ext-1", Email: "a@example.com", DisplayName: "Alice A", Active: true}
	if err := provisioner.UpsertUser(ctx, updated); err != nil {
		t.Fatal(err)
	}
	if provisioner.Users["ext-1"].DisplayName != "Alice A" {
		t.Fatalf("upsert did not update: %+v", provisioner.Users["ext-1"])
	}
	if err := provisioner.DeactivateUser(ctx, "ext-1"); err != nil {
		t.Fatal(err)
	}
	if provisioner.Users["ext-1"].Active {
		t.Fatal("deactivate did not mark inactive")
	}
	if err := provisioner.DeactivateUser(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deactivate missing = %v", err)
	}
	if provisioner.Calls != 4 {
		t.Fatalf("calls = %d", provisioner.Calls)
	}
}
