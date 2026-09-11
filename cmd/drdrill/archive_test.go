package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArchiveReportWritesAndPrunes(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	first, err := archiveReport(dir, "region-a", "tenant-x", []byte(`{"a":1}`), base, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("first report missing: %v", err)
	}
	second, err := archiveReport(dir, "region-a", "tenant-x", []byte(`{"a":2}`), base.Add(60*time.Second), 2)
	if err != nil {
		t.Fatal(err)
	}
	third, err := archiveReport(dir, "region-a", "tenant-x", []byte(`{"a":3}`), base.Add(120*time.Second), 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first); err == nil {
		t.Fatal("oldest report not pruned")
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatalf("second report missing after prune: %v", err)
	}
	if _, err := os.Stat(third); err != nil {
		t.Fatalf("third report missing: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "drdrill-*.json"))
	if len(matches) != 2 {
		t.Fatalf("archived files = %d", len(matches))
	}
}

func TestArchiveReportSanitizesNames(t *testing.T) {
	dir := t.TempDir()
	path, err := archiveReport(dir, "region/a:b", "tenant x", []byte(`{}`), time.Unix(1, 0), 1)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(path)
	if name != "drdrill-region_a_b-tenant_x-19700101T000001Z.json" {
		t.Fatalf("archive name = %s", name)
	}
}

func TestArchiveReportSkipsWhenNoDir(t *testing.T) {
	path, err := archiveReport("", "region", "tenant", []byte(`{}`), time.Unix(1, 0), 1)
	if err != nil || path != "" {
		t.Fatalf("path=%q err=%v", path, err)
	}
}
