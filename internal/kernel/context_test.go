package kernel

import (
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
)

func TestRequestContextKeepsCapturedSnapshot(t *testing.T) {
	snapshot := &runtime.TenantRuntimeSnapshot{TenantID: "tenant-1", Version: 7}
	ctx := &RequestContext{
		RequestID:  "request-1",
		ReceivedAt: time.Unix(1, 0),
		Snapshot:   snapshot,
		Interaction: &interaction.Context{
			Kind:     interaction.KindModel,
			TenantID: "tenant-1",
		},
	}

	if ctx.Snapshot != snapshot {
		t.Fatal("request context did not retain the captured snapshot pointer")
	}
}
