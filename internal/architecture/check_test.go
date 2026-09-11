package architecture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckImportsRejectsForbiddenDependency(t *testing.T) {
	manifest := Manifest{ModulePath: "github.com/F31/liteAIG"}
	rules := ImportRules{Rules: []ImportRule{{
		Name: "kernel-is-business-neutral",
		From: "internal/kernel",
		Deny: []string{"internal/finops"},
	}}}
	packages := []Package{{
		ImportPath: "github.com/F31/liteAIG/internal/kernel/pipeline",
		Imports:    []string{"github.com/F31/liteAIG/internal/finops/budget"},
	}}

	violations := CheckImports(manifest, rules, packages)
	if len(violations) != 1 || !strings.Contains(violations[0].String(), "forbidden import") {
		t.Fatalf("CheckImports() = %+v", violations)
	}
}

func TestCheckImportsAllowsKernelDependency(t *testing.T) {
	manifest := Manifest{ModulePath: "github.com/F31/liteAIG"}
	rules := ImportRules{Rules: []ImportRule{{
		Name: "kernel-is-business-neutral",
		From: "internal/kernel",
		Deny: []string{"internal/finops"},
	}}}
	packages := []Package{{
		ImportPath: "github.com/F31/liteAIG/internal/kernel/pipeline",
		Imports:    []string{"github.com/F31/liteAIG/internal/kernel/interaction"},
	}}

	if violations := CheckImports(manifest, rules, packages); len(violations) != 0 {
		t.Fatalf("CheckImports() = %+v", violations)
	}
}

func TestCheckModulesRejectsPackageWithoutOwner(t *testing.T) {
	manifest := Manifest{
		ModulePath: "github.com/F31/liteAIG",
		Modules:    []Module{{Name: "kernel", Path: "internal/kernel"}},
	}
	packages := []Package{{ImportPath: "github.com/F31/liteAIG/internal/unowned"}}

	violations := CheckModules(manifest, packages)
	if len(violations) != 1 || violations[0].Rule != "module-owner" {
		t.Fatalf("CheckModules() = %+v", violations)
	}
}

func TestCheckTableOwnerModulesRejectsUnknownOwner(t *testing.T) {
	manifest := Manifest{Modules: []Module{{Name: "tenancy", Path: "internal/tenancy"}}}
	owners := TableOwners{Tables: map[string]string{"usage_facts": "finops"}}

	violations := CheckTableOwnerModules(manifest, owners)
	if len(violations) != 1 || !strings.Contains(violations[0].Detail, "finops") {
		t.Fatalf("CheckTableOwnerModules() = %+v", violations)
	}
}

func TestCheckMigrationOwners(t *testing.T) {
	root := t.TempDir()
	migrations := filepath.Join(root, "migrations")
	if err := os.Mkdir(migrations, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(migrations, "001_tenants.sql"),
		[]byte("-- owner: identity\nCREATE TABLE tenants (id TEXT);\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	violations, err := CheckMigrationOwners(root, TableOwners{Tables: map[string]string{"tenants": "tenancy"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || !strings.Contains(violations[0].Detail, "belongs to tenancy") {
		t.Fatalf("CheckMigrationOwners() = %+v", violations)
	}
}

func TestCheckDirectSQLFlagsCrossModuleTableUse(t *testing.T) {
	root := t.TempDir()
	manifest := Manifest{ModulePath: "github.com/F31/liteAIG", Modules: []Module{
		{Name: "controlplane", Path: "internal/controlplane"},
		{Name: "storage", Path: "internal/platform/storage"},
		{Name: "setup", Path: "internal/controlplane/setup"},
	}}
	owners := TableOwners{Tables: map[string]string{"tenants": "tenancy", "local_admins": "identity"}}

	files := map[string]string{
		"internal/controlplane/users/svc.go":              "package users\nvar q = \"SELECT id FROM tenants WHERE id = $1\"\n",
		"internal/platform/storage/sqlrepo/tenancy.go":    "package sqlrepo\nvar q = \"SELECT id FROM tenants WHERE id = $1\"\n",
		"internal/controlplane/setup/contracttest/run.go": "package contracttest\nvar q = \"SELECT password_hash FROM local_admins WHERE id = $1\"\n",
	}
	for path, content := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	violations := CheckDirectSQL(root, manifest, owners)
	if len(violations) != 1 || !strings.Contains(violations[0].Detail, "belongs to module tenancy") {
		t.Fatalf("CheckDirectSQL() = %+v", violations)
	}
}
