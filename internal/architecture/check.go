// Package architecture enforces module and table ownership boundaries.
package architecture

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Manifest describes the repository module namespace.
type Manifest struct {
	ModulePath string   `yaml:"module_path"`
	Modules    []Module `yaml:"modules"`
}

// Module is one code ownership boundary.
type Module struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
}

// ImportRules contains forbidden direct import relationships.
type ImportRules struct {
	Rules []ImportRule `yaml:"rules"`
}

// ImportRule denies imports from one package prefix to selected prefixes.
type ImportRule struct {
	Name string   `yaml:"name"`
	From string   `yaml:"from"`
	Deny []string `yaml:"deny"`
}

// TableOwners maps persistent tables to their owning module.
type TableOwners struct {
	Tables map[string]string `yaml:"tables"`
}

// Package is the direct import data needed from go list.
type Package struct {
	ImportPath string
	Imports    []string
}

// Violation is one executable architecture boundary failure.
type Violation struct {
	Rule    string
	Package string
	Detail  string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: %s: %s", v.Rule, v.Package, v.Detail)
}

// Load reads all architecture manifests from the repository root.
func Load(root string) (Manifest, ImportRules, TableOwners, error) {
	var modules Manifest
	if err := readYAML(filepath.Join(root, "architecture", "modules.yaml"), &modules); err != nil {
		return Manifest{}, ImportRules{}, TableOwners{}, err
	}
	var rules ImportRules
	if err := readYAML(filepath.Join(root, "architecture", "forbidden-imports.yaml"), &rules); err != nil {
		return Manifest{}, ImportRules{}, TableOwners{}, err
	}
	var owners TableOwners
	if err := readYAML(filepath.Join(root, "architecture", "table-owners.yaml"), &owners); err != nil {
		return Manifest{}, ImportRules{}, TableOwners{}, err
	}
	if modules.ModulePath == "" {
		return Manifest{}, ImportRules{}, TableOwners{}, fmt.Errorf("architecture/modules.yaml: module_path is required")
	}
	return modules, rules, owners, nil
}

func readYAML(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, value); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// ListPackages returns direct imports for repository packages.
func ListPackages(root string) ([]Package, error) {
	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}

	decoder := json.NewDecoder(strings.NewReader(string(output)))
	var packages []Package
	for {
		var pkg Package
		if err := decoder.Decode(&pkg); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode go list output: %w", err)
		}
		packages = append(packages, pkg)
	}
	return packages, nil
}

// CheckImports evaluates direct imports against the deny rules.
func CheckImports(manifest Manifest, rules ImportRules, packages []Package) []Violation {
	var violations []Violation
	for _, pkg := range packages {
		from, ok := internalPath(manifest.ModulePath, pkg.ImportPath)
		if !ok {
			continue
		}
		for _, rule := range rules.Rules {
			if !pathMatches(from, rule.From) {
				continue
			}
			for _, imported := range pkg.Imports {
				to, ok := internalPath(manifest.ModulePath, imported)
				if !ok {
					continue
				}
				for _, denied := range rule.Deny {
					if pathMatches(to, denied) {
						violations = append(violations, Violation{
							Rule:    rule.Name,
							Package: from,
							Detail:  "forbidden import " + to,
						})
					}
				}
			}
		}
	}
	return violations
}

// CheckModules rejects repository packages without a declared module owner.
func CheckModules(manifest Manifest, packages []Package) []Violation {
	var violations []Violation
	for _, pkg := range packages {
		path, ok := internalPath(manifest.ModulePath, pkg.ImportPath)
		if !ok || !strings.HasPrefix(path, "internal/") {
			continue
		}
		owned := false
		for _, module := range manifest.Modules {
			if pathMatches(path, module.Path) {
				owned = true
				break
			}
		}
		if !owned {
			violations = append(violations, Violation{
				Rule:    "module-owner",
				Package: path,
				Detail:  "package has no owner in architecture/modules.yaml",
			})
		}
	}
	return violations
}

// CheckTableOwnerModules rejects table owners that are not declared modules.
func CheckTableOwnerModules(manifest Manifest, owners TableOwners) []Violation {
	known := make(map[string]bool, len(manifest.Modules))
	for _, module := range manifest.Modules {
		known[module.Name] = true
	}
	var violations []Violation
	for table, owner := range owners.Tables {
		if !known[owner] {
			violations = append(violations, Violation{
				Rule:    "table-owner",
				Package: table,
				Detail:  "owner " + owner + " is not declared in architecture/modules.yaml",
			})
		}
	}
	return violations
}

func internalPath(modulePath, importPath string) (string, bool) {
	prefix := strings.TrimSuffix(modulePath, "/") + "/"
	if !strings.HasPrefix(importPath, prefix) {
		return "", false
	}
	return strings.TrimPrefix(importPath, prefix), true
}

func pathMatches(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, strings.TrimSuffix(prefix, "/")+"/")
}

var tableStatement = regexp.MustCompile(`(?i)\b(?:create\s+table(?:\s+if\s+not\s+exists)?|alter\s+table)\s+["` + "`" + `]?([a-zA-Z0-9_]+)`)

// CheckMigrationOwners verifies migration metadata against the table-owner manifest.
func CheckMigrationOwners(root string, owners TableOwners) ([]Violation, error) {
	migrationRoot := filepath.Join(root, "migrations")
	entries, err := os.ReadDir(migrationRoot)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var violations []Violation
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		path := filepath.Join(migrationRoot, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		owner := migrationOwner(string(data))
		matches := tableStatement.FindAllStringSubmatch(string(data), -1)
		for _, match := range matches {
			table := strings.ToLower(match[1])
			expected, known := owners.Tables[table]
			switch {
			case !known:
				violations = append(violations, Violation{"table-owner", entry.Name(), "table " + table + " has no declared owner"})
			case owner == "":
				violations = append(violations, Violation{"table-owner", entry.Name(), "missing -- owner: " + expected})
			case owner != expected:
				violations = append(violations, Violation{"table-owner", entry.Name(), fmt.Sprintf("table %s belongs to %s, migration declares %s", table, expected, owner)})
			}
		}
	}
	return violations, nil
}

func migrationOwner(contents string) string {
	scanner := bufio.NewScanner(strings.NewReader(contents))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "--") {
			return ""
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "--"))
		if value, ok := strings.CutPrefix(line, "owner:"); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// SortViolations makes command and test output deterministic.
func SortViolations(violations []Violation) {
	sort.Slice(violations, func(i, j int) bool {
		return violations[i].String() < violations[j].String()
	})
}

// tableReference finds SQL table references (FROM/JOIN/INTO/UPDATE <name>)
// in string literals of Go source. Only names declared in the table-owner
// manifest are reported, keeping the check free of false positives.
var tableReference = regexp.MustCompile(`(?i)\b(?:FROM|JOIN|INTO|UPDATE)\s+([a-zA-Z_][a-zA-Z0-9_]*)`)

// CheckDirectSQL flags Go files outside the storage adapter layer that
// reference tables owned by another module. Contract-test harnesses are
// exempt: they exist to verify cross-module repository behavior.
func CheckDirectSQL(root string, modules Manifest, owners TableOwners) []Violation {
	known := make(map[string]string, len(owners.Tables))
	for table, owner := range owners.Tables {
		known[table] = owner
	}
	internalRoot := filepath.Join(root, "internal")
	var violations []Violation
	_ = filepath.WalkDir(internalRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		if strings.HasPrefix(rel, "internal/platform/storage") || strings.Contains(rel, "contracttest") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, match := range tableReference.FindAllStringSubmatch(string(data), -1) {
			table := strings.ToLower(match[1])
			owner, knownTable := known[table]
			if !knownTable {
				continue
			}
			if moduleForPath(modules, rel) == owner {
				continue
			}
			violations = append(violations, Violation{
				Rule:    "direct-sql",
				Package: rel,
				Detail:  "table " + table + " belongs to module " + owner,
			})
		}
		return nil
	})
	return violations
}

func moduleForPath(manifest Manifest, path string) string {
	// Longest declared module path wins.
	best, bestLen := "", -1
	for _, module := range manifest.Modules {
		if pathMatches(path, module.Path) && len(module.Path) > bestLen {
			best, bestLen = module.Name, len(module.Path)
		}
	}
	return best
}
