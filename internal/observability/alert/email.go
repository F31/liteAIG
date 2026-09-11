package alert

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/F31/liteAIG/internal/platform/mail"
)

// EmailNotifier delivers one fired alert as a deterministic plain-text email.
// The operator supplies the relay (via mail.Sender) and a recipient; both are
// process-global, so the Notifier is tenant-agnostic and never stores secrets
// beyond what the Alert already carries.
type EmailNotifier struct {
	Sender mail.Sender
	To     string
}

var _ Notifier = (*EmailNotifier)(nil)

// Notify composes and sends the alert email. An empty recipient is a no-op.
func (n *EmailNotifier) Notify(ctx context.Context, alert Alert) error {
	if n.Sender == nil || n.To == "" {
		return nil
	}
	subject, body := EmailTemplateText(alert)
	return n.Sender.Send(ctx, n.To, subject, body)
}

// EmailTemplateText renders a fired alert into a deterministic subject and
// plain-text body. Evidence values are quoted verbatim (they are already the
// redacted alert fact set, never request bodies).
func EmailTemplateText(alert Alert) (subject, body string) {
	severity := alert.Severity
	if severity == "" {
		severity = "unknown"
	}
	name := alert.RuleID
	if name == "" {
		name = alert.ID
	}
	subject = fmt.Sprintf("[LiteAIG %s] %s", strings.ToUpper(severity), name)
	var builder strings.Builder
	builder.WriteString("LiteAIG alert\n")
	fmt.Fprintf(&builder, "id: %s\n", alert.ID)
	fmt.Fprintf(&builder, "rule: %s\n", alert.RuleID)
	fmt.Fprintf(&builder, "severity: %s\n", alert.Severity)
	fmt.Fprintf(&builder, "status: %s\n", alert.Status)
	if alert.Message != "" {
		fmt.Fprintf(&builder, "message: %s\n", alert.Message)
	}
	if !alert.FiredAt.IsZero() {
		fmt.Fprintf(&builder, "fired_at: %s\n", alert.FiredAt.UTC().Format(time.RFC3339))
	}
	if !alert.WindowStart.IsZero() && !alert.WindowEnd.IsZero() {
		fmt.Fprintf(&builder, "window: %s .. %s\n", alert.WindowStart.UTC().Format(time.RFC3339), alert.WindowEnd.UTC().Format(time.RFC3339))
	}
	if len(alert.Evidence) > 0 {
		builder.WriteString("evidence:\n")
		keys := make([]string, 0, len(alert.Evidence))
		for key := range alert.Evidence {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(&builder, "  %s: %s\n", key, alert.Evidence[key])
		}
	}
	return subject, strings.TrimRight(builder.String(), "\n")
}
