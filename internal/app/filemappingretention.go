package app

import (
	"context"
	"time"

	controlconfig "github.com/F31/liteAIG/internal/controlplane/config"
)

type fileMappingRetentionConfig interface {
	SystemConfig(context.Context) (controlconfig.SystemConfig, error)
}

type fileMappingPurger interface {
	PurgeFileMappingsOlderThan(context.Context, time.Time) (int64, error)
}

// sweepFileMappingRetention removes only LiteAIG's local file authorization
// mappings. Upstream provider files are intentionally outside this interface.
func sweepFileMappingRetention(ctx context.Context, configs fileMappingRetentionConfig, mappings fileMappingPurger, now time.Time) (int64, bool, error) {
	document, err := configs.SystemConfig(ctx)
	if err != nil {
		return 0, false, err
	}
	days := document.FileMappingRetentionDays
	if days <= 0 {
		return 0, false, nil
	}
	removed, err := mappings.PurgeFileMappingsOlderThan(ctx, now.AddDate(0, 0, -days))
	return removed, true, err
}
