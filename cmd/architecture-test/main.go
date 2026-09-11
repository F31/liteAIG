package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/F31/liteAIG/internal/architecture"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	manifest, rules, owners, err := architecture.Load(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	packages, err := architecture.ListPackages(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	violations := architecture.CheckModules(manifest, packages)
	violations = append(violations, architecture.CheckTableOwnerModules(manifest, owners)...)
	violations = append(violations, architecture.CheckImports(manifest, rules, packages)...)
	violations = append(violations, architecture.CheckDirectSQL(root, manifest, owners)...)
	migrationViolations, err := architecture.CheckMigrationOwners(root, owners)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	violations = append(violations, migrationViolations...)
	apiSnapshot := filepath.Join(root, "architecture", "exported.json")
	if _, err := os.Stat(apiSnapshot); err == nil {
		apiViolations, apiErr := checkAPISnapshot(apiSnapshot)
		if apiErr != nil {
			fmt.Fprintln(os.Stderr, apiErr)
			os.Exit(1)
		}
		violations = append(violations, apiViolations...)
	}
	architecture.SortViolations(violations)
	if len(violations) > 0 {
		for _, violation := range violations {
			fmt.Fprintln(os.Stderr, violation)
		}
		os.Exit(1)
	}
	fmt.Println("architecture boundaries: ok")
}

// checkAPISnapshot compares the live exported surface of ./pkg/... against the
// committed golden snapshot and reports breaking removals.
func checkAPISnapshot(snapshotPath string) ([]architecture.Violation, error) {
	snapshot, err := architecture.LoadExportedSnapshot(snapshotPath)
	if err != nil {
		return nil, err
	}
	packages, err := architecture.ListModulePackages(".", "./pkg/...")
	if err != nil {
		return nil, err
	}
	live, err := architecture.ExportedSurface(packages)
	if err != nil {
		return nil, err
	}
	return architecture.CompareSurface(snapshot, live), nil
}
