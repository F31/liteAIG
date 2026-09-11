package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// archiveReport writes one drill report into archiveDir using a stable,
// timestamped filename and trims the directory to the most recent keep
// reports (globally). It returns the written path.
func archiveReport(archiveDir string, region, tenant string, encoded []byte, now time.Time, keep int) (string, error) {
	if archiveDir == "" {
		return "", nil
	}
	if keep <= 0 {
		keep = 30
	}
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return "", fmt.Errorf("create archive dir: %w", err)
	}
	stamp := now.UTC().Format("20060102T150405Z")
	safeRegion := sanitizeName(region)
	safeTenant := sanitizeName(tenant)
	path := filepath.Join(archiveDir, fmt.Sprintf("drdrill-%s-%s-%s.json", safeRegion, safeTenant, stamp))
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return "", fmt.Errorf("write archive report: %w", err)
	}
	pruneArchives(archiveDir, "drdrill-*.json", keep)
	return path, nil
}

// pruneArchives keeps only the newest keep files matching pattern within dir.
func pruneArchives(dir, pattern string, keep int) {
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return
	}
	if len(matches) <= keep {
		return
	}
	sort.Strings(matches)
	for _, stale := range matches[:len(matches)-keep] {
		_ = os.Remove(stale)
	}
}

// sanitizeName keeps a path component safe for filenames.
func sanitizeName(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	if builder.Len() == 0 {
		return "unknown"
	}
	return builder.String()
}
