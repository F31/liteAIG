package spool

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/finops/accounting"
)

func testRecord(id string) accounting.SpoolRecord {
	return accounting.SpoolRecord{
		EventID:   id,
		RequestID: "req-" + id,
		TenantID:  "tenant-a",
		ProjectID: "project-a",
		EventTS:   time.Unix(1000, 0),
		Facts:     accounting.Facts{UsageEventID: id, RequestID: "req-" + id, TenantID: "tenant-a", ProjectID: "project-a", LogicalModel: "chat", Outcome: "success", CompletedAt: time.Unix(1000, 0)},
	}
}

func TestFileSpoolAppendPendingCommitTruncate(t *testing.T) {
	ctx := context.Background()
	s, err := New(Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for i := 0; i < 5; i++ {
		if err := s.Append(ctx, testRecord("e"+string(rune('0'+i)))); err != nil {
			t.Fatalf("Append()=%v", err)
		}
	}
	pending, err := s.Pending(ctx, 100)
	if err != nil || len(pending) != 5 {
		t.Fatalf("Pending()=%d,%v", len(pending), err)
	}
	stats, err := s.Stats(ctx)
	if err != nil || stats.Events != 5 || stats.Bytes == 0 {
		t.Fatalf("Stats()=%+v,%v", stats, err)
	}
	if err := s.Commit(ctx, []string{"e0", "e1"}); err != nil {
		t.Fatal(err)
	}
	pending, _ = s.Pending(ctx, 100)
	if len(pending) != 3 {
		t.Fatalf("Pending() after commit = %d, want 3", len(pending))
	}
	// All events committed → segment truncated.
	if err := s.Commit(ctx, []string{"e2", "e3", "e4"}); err != nil {
		t.Fatal(err)
	}
	if got := s.SegmentCount(); got != 0 {
		t.Fatalf("SegmentCount() = %d, want 0 (truncated)", got)
	}
}

func TestFileSpoolRestartPreservesPending(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(ctx, testRecord("r1")); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(ctx, testRecord("r2")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	pending, err := s2.Pending(ctx, 10)
	if err != nil || len(pending) != 2 {
		t.Fatalf("Pending() after restart = %d,%v", len(pending), err)
	}
	if pending[0].EventID != "r1" || pending[1].EventID != "r2" {
		t.Fatalf("order = %+v", pending)
	}
	events, bytes, err := s2.TenantUsage(ctx, "tenant-a")
	if err != nil || events != 2 || bytes == 0 {
		t.Fatalf("TenantUsage() = %d,%d,%v", events, bytes, err)
	}
}

func TestFileSpoolTornTailRecoveredOnLoad(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(ctx, testRecord("t1")); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(ctx, testRecord("t2")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash mid-append: a partial final line with no newline.
	f, err := os.OpenFile(filepath.Join(dir, "segment-000001.wal"), os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"event_id":"t3","req`); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatalf("re-open after torn tail: %v", err)
	}
	defer s2.Close()
	pending, err := s2.Pending(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 || pending[0].EventID != "t1" || pending[1].EventID != "t2" {
		t.Fatalf("pending = %+v, want the two complete events", pending)
	}
}

func TestFileSpoolMiddleCorruptionRejected(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(ctx, testRecord("c1")); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(ctx, testRecord("c2")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Flip a byte inside the first (complete) line: not a torn tail.
	path := filepath.Join(dir, "segment-000001.wal")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[10] = 'X'
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Dir: dir}); err == nil {
		t.Fatal("re-open accepted a corrupt middle record")
	}
}

func TestFileSpoolStatsTrackPendingOnly(t *testing.T) {
	ctx := context.Background()
	s, err := New(Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 4; i++ {
		if err := s.Append(ctx, testRecord("p"+string(rune('0'+i)))); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := s.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Events != 4 || stats.Bytes == 0 || stats.OldestAge <= 0 {
		t.Fatalf("stats = %+v, want 4 pending events with age", stats)
	}
	if err := s.Commit(ctx, []string{"p0", "p1"}); err != nil {
		t.Fatal(err)
	}
	stats, _ = s.Stats(ctx)
	if stats.Events != 2 {
		t.Fatalf("stats after commit = %+v, want 2 pending", stats)
	}
	events, bytes, err := s.TenantUsage(ctx, "tenant-a")
	if err != nil || events != 2 || bytes == 0 {
		t.Fatalf("tenant usage = %d,%d,%v, want 2 pending", events, bytes, err)
	}
}

func TestFileSpoolAppendAfterFullTruncation(t *testing.T) {
	ctx := context.Background()
	s, err := New(Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Append(ctx, testRecord("x1")); err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(ctx, []string{"x1"}); err != nil {
		t.Fatal(err)
	}
	if got := s.SegmentCount(); got != 0 {
		t.Fatalf("SegmentCount() = %d, want 0", got)
	}
	// A fresh segment must be created; this used to index an empty slice.
	if err := s.Append(ctx, testRecord("x2")); err != nil {
		t.Fatalf("Append after full truncation: %v", err)
	}
	pending, err := s.Pending(ctx, 10)
	if err != nil || len(pending) != 1 || pending[0].EventID != "x2" {
		t.Fatalf("pending = %+v,%v, want x2", pending, err)
	}
}

func TestFileSpoolQuotaEnforcedByPolicy(t *testing.T) {
	ctx := context.Background()
	s, err := New(Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	policy := accounting.SpoolPolicy{Mode: "hard", WarnThreshold: 0.7, CriticalThreshold: 0.9, HardThreshold: 1.0, QuotaBytes: 10}
	repo := accounting.NewSpoolRepository(s, policy)
	if _, err := repo.Finalize(ctx, testRecord("q1").Facts); err != accounting.ErrSpoolFull {
		t.Fatalf("Finalize() over quota error=%v, want ErrSpoolFull", err)
	}
	// A large quota allows appends.
	repo2 := accounting.NewSpoolRepository(s, accounting.SpoolPolicy{Mode: "soft", QuotaBytes: 1 << 20})
	if _, err := repo2.Finalize(ctx, testRecord("q2").Facts); err != nil {
		t.Fatalf("Finalize() with headroom error=%v", err)
	}
}
