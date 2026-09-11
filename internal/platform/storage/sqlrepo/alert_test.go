package sqlrepo

import (
	"context"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/observability/alert"
	"github.com/F31/liteAIG/internal/tenancy"
)

func TestAlertStoreNotificationSettingsCRUD(t *testing.T) {
	store := a2aTaskTestStore(t, "notification-settings")
	ctx := context.Background()
	seedA2ATaskTenant(t, store, "tenant-notify")
	seedA2ATaskTenant(t, store, "tenant-other")
	scope := tenancy.TenantScope{TenantID: "tenant-notify"}
	other := tenancy.TenantScope{TenantID: "tenant-other"}

	if _, err := store.Alerts.GetNotificationSettings(ctx, scope); err != tenancy.ErrNotFound {
		t.Fatalf("empty get err = %v, want ErrNotFound", err)
	}
	settings := alert.NotificationSettings{TenantID: scope.TenantID, WebhookURL: "https://example.com/hook", Enabled: true, UpdatedBy: "admin", UpdatedAt: time.Unix(100, 0)}
	if err := store.Alerts.UpsertNotificationSettings(ctx, scope, settings); err != nil {
		t.Fatal(err)
	}
	got, err := store.Alerts.GetNotificationSettings(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if got.WebhookURL != settings.WebhookURL || !got.Enabled || got.UpdatedBy != "admin" {
		t.Fatalf("settings = %+v", got)
	}
	if _, err := store.Alerts.GetNotificationSettings(ctx, other); err != tenancy.ErrNotFound {
		t.Fatalf("cross-tenant get err = %v, want ErrNotFound", err)
	}
	settings.WebhookURL = "https://example.com/updated"
	settings.Enabled = false
	if err := store.Alerts.UpsertNotificationSettings(ctx, scope, settings); err != nil {
		t.Fatal(err)
	}
	got, err = store.Alerts.GetNotificationSettings(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if got.WebhookURL != settings.WebhookURL || got.Enabled {
		t.Fatalf("updated settings = %+v", got)
	}
	if err := store.Alerts.DeleteNotificationSettings(ctx, scope); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Alerts.GetNotificationSettings(ctx, scope); err != tenancy.ErrNotFound {
		t.Fatalf("after delete err = %v, want ErrNotFound", err)
	}
}
