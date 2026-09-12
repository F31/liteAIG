package app

import (
	"context"
	"errors"
	"testing"
	"time"

	controlconfig "github.com/F31/liteAIG/internal/controlplane/config"
)

type fileRetentionConfigStub struct {
	document controlconfig.SystemConfig
	err      error
}

func (s fileRetentionConfigStub) SystemConfig(context.Context) (controlconfig.SystemConfig, error) {
	return s.document, s.err
}

type fileMappingPurgerStub struct {
	before time.Time
	calls  int
	rows   int64
	err    error
}

func (s *fileMappingPurgerStub) PurgeFileMappingsOlderThan(_ context.Context, before time.Time) (int64, error) {
	s.calls++
	s.before = before
	return s.rows, s.err
}

func TestFileMappingRetentionSweep(t *testing.T) {
	now := time.Date(2026, time.September, 12, 12, 0, 0, 0, time.UTC)
	t.Run("disabled", func(t *testing.T) {
		purger := &fileMappingPurgerStub{}
		removed, enabled, err := sweepFileMappingRetention(context.Background(), fileRetentionConfigStub{}, purger, now)
		if err != nil || enabled || removed != 0 || purger.calls != 0 {
			t.Fatalf("removed=%d enabled=%t calls=%d err=%v", removed, enabled, purger.calls, err)
		}
	})

	t.Run("configured", func(t *testing.T) {
		purger := &fileMappingPurgerStub{rows: 3}
		config := fileRetentionConfigStub{document: controlconfig.SystemConfig{FileMappingRetentionDays: 30}}
		removed, enabled, err := sweepFileMappingRetention(context.Background(), config, purger, now)
		wantCutoff := now.AddDate(0, 0, -30)
		if err != nil || !enabled || removed != 3 || purger.calls != 1 || !purger.before.Equal(wantCutoff) {
			t.Fatalf("removed=%d enabled=%t calls=%d cutoff=%s want=%s err=%v", removed, enabled, purger.calls, purger.before, wantCutoff, err)
		}
	})

	t.Run("config error", func(t *testing.T) {
		purger := &fileMappingPurgerStub{}
		want := errors.New("config unavailable")
		_, enabled, err := sweepFileMappingRetention(context.Background(), fileRetentionConfigStub{err: want}, purger, now)
		if !errors.Is(err, want) || enabled || purger.calls != 0 {
			t.Fatalf("enabled=%t calls=%d err=%v", enabled, purger.calls, err)
		}
	})
}
