package migrations

import "testing"

func TestEveryMigrationDeclaresOwner(t *testing.T) {
	versions := Versions()
	if len(versions) == 0 {
		t.Fatal("no embedded migrations")
	}
	for _, version := range versions {
		if owner, err := Owner(version); err != nil || owner == "" {
			t.Fatalf("Owner(%q) = %q, %v", version, owner, err)
		}
	}
}
