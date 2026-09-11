package architecture

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ExportedSymbol is one exported top-level declaration (or exported method) in
// a public module package. It is the unit of the module API stability gate.
type ExportedSymbol struct {
	Package string `json:"package"`
	Kind    string `json:"kind"` // func, method, type, var, const
	Name    string `json:"name"`
	// Method names the receiver type when Kind is "method".
	Method string `json:"method,omitempty"`
}

// String returns a stable identity used for set comparison.
func (s ExportedSymbol) String() string {
	return s.Package + ":" + s.Kind + ":" + s.Method + ":" + s.Name
}

// ModulePackage is the go list shape needed to read a package's source.
type ModulePackage struct {
	ImportPath string
	Dir        string
	GoFiles    []string
}

// ListModulePackages returns go list -json for the given package patterns.
func ListModulePackages(root string, patterns ...string) ([]ModulePackage, error) {
	args := append([]string{"list", "-json"}, patterns...)
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	var packages []ModulePackage
	for {
		var pkg ModulePackage
		if err := decoder.Decode(&pkg); err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
		packages = append(packages, pkg)
	}
	return packages, nil
}

// ExportedSurface returns the exported top-level surface of each package.
func ExportedSurface(packages []ModulePackage) ([]ExportedSymbol, error) {
	var surface []ExportedSymbol
	for _, pkg := range packages {
		importPath := pkg.ImportPath
		for _, name := range pkg.GoFiles {
			path := filepath.Join(pkg.Dir, name)
			symbols, err := exportedFileSymbols(importPath, path)
			if err != nil {
				return nil, err
			}
			surface = append(surface, symbols...)
		}
	}
	SortSymbols(surface)
	return surface, nil
}

// exportedFileSymbols parses one Go source file and returns its exported
// top-level declarations and exported methods on locally declared types.
func exportedFileSymbols(importPath, path string) ([]ExportedSymbol, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var symbols []ExportedSymbol
	for _, decl := range file.Decls {
		switch node := decl.(type) {
		case *ast.FuncDecl:
			if node.Name == nil || !isExported(node.Name.Name) {
				continue
			}
			if node.Recv != nil && len(node.Recv.List) > 0 {
				receiver := receiverTypeName(node.Recv.List[0].Type)
				if receiver == "" {
					continue
				}
				symbols = append(symbols, ExportedSymbol{Package: importPath, Kind: "method", Method: receiver, Name: node.Name.Name})
			} else {
				symbols = append(symbols, ExportedSymbol{Package: importPath, Kind: "func", Name: node.Name.Name})
			}
		case *ast.GenDecl:
			for _, spec := range node.Specs {
				switch value := spec.(type) {
				case *ast.TypeSpec:
					if value.Name != nil && isExported(value.Name.Name) {
						symbols = append(symbols, ExportedSymbol{Package: importPath, Kind: "type", Name: value.Name.Name})
					}
				case *ast.ValueSpec:
					for _, name := range value.Names {
						if isExported(name.Name) {
							kind := "var"
							if node.Tok == token.CONST {
								kind = "const"
							}
							symbols = append(symbols, ExportedSymbol{Package: importPath, Kind: kind, Name: name.Name})
						}
					}
				}
			}
		}
	}
	return symbols, nil
}

func receiverTypeName(expr ast.Expr) string {
	var name string
	strip := func(node ast.Expr) bool {
		ident, ok := node.(*ast.Ident)
		if ok {
			name = ident.Name
		}
		return ok
	}
	switch target := expr.(type) {
	case *ast.StarExpr:
		return receiverTypeName(target.X)
	case *ast.IndexExpr:
		return receiverTypeName(target.X)
	case *ast.IndexListExpr:
		return receiverTypeName(target.X)
	case *ast.Ident:
		_ = strip(target)
		return name
	default:
		return name
	}
}

func isExported(name string) bool {
	if name == "" {
		return false
	}
	first, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(first)
}

// SortSymbols orders symbols deterministically for comparison.
func SortSymbols(symbols []ExportedSymbol) {
	sort.Slice(symbols, func(i, j int) bool {
		return symbols[i].String() < symbols[j].String()
	})
}

// CompareSurface reports exported symbols present before but absent after:
// these are module API removals that would break consumers. Additions are
// allowed and never flagged.
func CompareSurface(before, after []ExportedSymbol) []Violation {
	present := make(map[string]bool, len(after))
	for _, symbol := range after {
		present[symbol.String()] = true
	}
	var violations []Violation
	for _, symbol := range before {
		if !present[symbol.String()] {
			violations = append(violations, Violation{
				Rule:    "module-api-breaking-removal",
				Package: symbol.Package,
				Detail:  fmt.Sprintf("exported %s %s:%s was removed", symbol.Kind, symbol.Method, symbol.Name),
			})
		}
	}
	return violations
}

// LoadExportedSnapshot reads a committed module API snapshot.
func LoadExportedSnapshot(path string) ([]ExportedSymbol, error) {
	var snapshot []ExportedSymbol
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(content, &snapshot); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return snapshot, nil
}

// SaveExportedSnapshot writes a module API snapshot deterministically.
func SaveExportedSnapshot(path string, symbols []ExportedSymbol, perm os.FileMode) error {
	SortSymbols(symbols)
	encoded, err := json.MarshalIndent(symbols, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, perm)
}
