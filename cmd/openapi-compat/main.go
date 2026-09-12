// Command openapi-compat rejects breaking changes between OpenAPI snapshots.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/F31/liteAIG/internal/architecture"
)

func main() {
	before := flag.String("before", "", "baseline OpenAPI 3.1 JSON file")
	after := flag.String("after", "", "candidate OpenAPI 3.1 JSON file")
	flag.Parse()
	if *before == "" || *after == "" {
		fmt.Fprintln(os.Stderr, "openapi-compat: -before and -after are required")
		os.Exit(2)
	}

	violations, err := architecture.CompareOpenAPIFiles(*before, *after)
	if err != nil {
		fmt.Fprintln(os.Stderr, "openapi-compat:", err)
		os.Exit(2)
	}
	if len(violations) > 0 {
		for _, violation := range violations {
			fmt.Fprintln(os.Stderr, violation)
		}
		os.Exit(1)
	}
	fmt.Println("OpenAPI compatibility: ok")
}
