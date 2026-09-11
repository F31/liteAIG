// Command api-snapshot snapshots the exported module API surface of the public
// pkg/ packages into architecture/exported.json (the committed golden used by
// cmd/architecture-test). Run with no flags to print the snapshot to stdout.
//
// Usage:
//
//	api-snapshot [-write] [-root /path/to/repo] [-out architecture/exported.json]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/F31/liteAIG/internal/architecture"
)

func main() {
	root := flag.String("root", ".", "repository root")
	out := flag.String("out", "architecture/exported.json", "snapshot path (relative to -root)")
	write := flag.Bool("write", false, "write the snapshot to -out instead of printing")
	flag.Parse()

	packages, err := architecture.ListModulePackages(*root, "./pkg/...")
	if err != nil {
		fmt.Fprintln(os.Stderr, "api-snapshot:", err)
		os.Exit(1)
	}
	surface, err := architecture.ExportedSurface(packages)
	if err != nil {
		fmt.Fprintln(os.Stderr, "api-snapshot:", err)
		os.Exit(1)
	}
	if !*write {
		snapshot := surface
		if len(snapshot) == 0 {
			fmt.Println("[]")
			return
		}
		architecture.SortSymbols(snapshot)
		for _, symbol := range snapshot {
			fmt.Printf("%s %s %s:%s\n", symbol.Package, symbol.Kind, symbol.Method, symbol.Name)
		}
		return
	}
	path := *out
	if !filepath.IsAbs(path) {
		path = filepath.Join(*root, path)
	}
	if err := architecture.SaveExportedSnapshot(path, surface, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "api-snapshot:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %d exported symbols to %s\n", len(surface), path)
}
