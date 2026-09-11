package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/platform/lkg"
)

func TestRunRequiresRegionAndTenant(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "--region and --tenant") {
		t.Fatalf("stderr=%s", stderr.String())
	}
}

func TestRunReadOnlyDrillJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--region", "region-a", "--tenant", "tenant", "--budget-mode", "global_soft"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"Region": "region-a"`) || !strings.Contains(stdout.String(), `"Ready": true`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestRunLKGDrillFailsWhenRegionBundleMissing(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--region", "region-a", "--tenant", "tenant", "--lkg-root", t.TempDir()}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"Ready": false`) || !strings.Contains(stderr.String(), "region is not DR-ready") {
		t.Fatalf("stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func TestRunLKGDrillWritesReportWhenReady(t *testing.T) {
	root := t.TempDir()
	store, err := lkg.NewRegionStore(root, verifyBundle)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), "region-a", "tenant-ref", lkg.Bundle{
		TenantID: "tenant-id", TenantRef: "tenant-ref", ConfigVersion: 3,
		SnapshotData: &runtime.TenantSnapshotData{TenantID: "tenant-id", TenantRef: "tenant-ref", Version: 3},
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "dr.json")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--region", "region-a", "--tenant", "tenant-ref", "--lkg-root", root, "--out", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	report, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(report), `"Ready": true`) {
		t.Fatalf("report=%s", string(report))
	}
}

func TestParseBudgetModeRejectsUnknown(t *testing.T) {
	if _, err := parseBudgetMode("eventual"); err == nil {
		t.Fatal("parseBudgetMode accepted unknown value")
	}
}
