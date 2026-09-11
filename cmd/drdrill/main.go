// Command drdrill runs the LiteAIG read-only DR checklist and emits a JSON
// report that can be scheduled from cron or CI.
//
// Usage:
//
//	drdrill --region region-a --tenant tenant-ref [--budget-mode global_soft]
//	        [--lkg-root /var/lib/liteaig/lkg] [--node-ready=true] [--out report.json]
//	        [--archive-dir /var/lib/liteaig/dr-reports] [--archive-keep 30]
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/F31/liteAIG/internal/platform/coordination"
	"github.com/F31/liteAIG/internal/platform/dr"
	"github.com/F31/liteAIG/internal/platform/lkg"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("drdrill", flag.ContinueOnError)
	flags.SetOutput(stderr)
	region := flags.String("region", "", "region name to validate (required)")
	tenant := flags.String("tenant", "", "tenant ref/id to validate (required; tenant ref when --lkg-root is set)")
	budgetMode := flags.String("budget-mode", string(coordination.BudgetRegional), "budget consistency mode: regional, global_soft, or global_hard")
	lkgRoot := flags.String("lkg-root", "", "optional region LKG root containing regions/<region>/<tenant>/active.bundle")
	nodeReady := flags.Bool("node-ready", true, "whether the target node/readiness probe is green")
	trafficSwitched := flags.Bool("traffic-switched", false, "record whether traffic has already been switched in this drill")
	outPath := flags.String("out", "", "optional path to write the JSON report")
	archiveDir := flags.String("archive-dir", "", "optional directory to archive dated JSON reports for cron/CI")
	archiveKeep := flags.Int("archive-keep", 30, "maximum number of archived reports to retain")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	mode, err := parseBudgetMode(*budgetMode)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if *region == "" || *tenant == "" {
		fmt.Fprintln(stderr, "drdrill: --region and --tenant are required")
		return 2
	}

	checklist := dr.NewChecklist(nil)
	env := dr.Env{Region: *region, TenantID: *tenant, BudgetMode: mode, TrafficSwitched: *trafficSwitched}
	readiness := func(context.Context, string, string, bool) bool { return *nodeReady }
	if *lkgRoot != "" {
		store, err := lkg.NewRegionStore(*lkgRoot, verifyBundle)
		if err != nil {
			fmt.Fprintln(stderr, "drdrill:", err)
			return 1
		}
		readiness = store.RegionReady
	}
	status, err := checklist.View(context.Background(), env, *nodeReady, readiness)
	encoded, jsonErr := json.MarshalIndent(status, "", "  ")
	if jsonErr != nil {
		fmt.Fprintln(stderr, "drdrill:", jsonErr)
		return 1
	}
	if *outPath != "" {
		if err := os.WriteFile(*outPath, encoded, 0o644); err != nil {
			fmt.Fprintln(stderr, "drdrill:", err)
			return 1
		}
	}
	if *archiveDir != "" {
		if _, err := archiveReport(*archiveDir, *region, *tenant, encoded, time.Now(), *archiveKeep); err != nil {
			fmt.Fprintln(stderr, "drdrill:", err)
			return 1
		}
	}
	fmt.Fprintln(stdout, string(encoded))
	if err != nil {
		fmt.Fprintln(stderr, "drdrill:", err)
		return 1
	}
	return 0
}

func parseBudgetMode(value string) (coordination.BudgetConsistency, error) {
	switch coordination.BudgetConsistency(value) {
	case coordination.BudgetRegional, coordination.BudgetGlobalSoft, coordination.BudgetGlobalHard:
		return coordination.BudgetConsistency(value), nil
	default:
		return "", fmt.Errorf("drdrill: unsupported --budget-mode %q", value)
	}
}

func verifyBundle(bundle *lkg.Bundle) error {
	if bundle == nil || bundle.TenantRef == "" || bundle.ConfigVersion <= 0 {
		return errors.New("invalid LKG bundle")
	}
	if bundle.SnapshotData == nil {
		return errors.New("missing LKG snapshot data")
	}
	if bundle.SnapshotData.TenantRef != "" && bundle.SnapshotData.TenantRef != bundle.TenantRef {
		return errors.New("LKG tenant ref mismatch")
	}
	if bundle.SnapshotData.TenantID == "" {
		return errors.New("missing LKG tenant id")
	}
	return nil
}
