package alert

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/platform/mail"
)

type recordingSender struct{ to, subject, body string }

func (s *recordingSender) Send(_ context.Context, to, subject, body string) error {
	s.to, s.subject, s.body = to, subject, body
	return nil
}

func TestEmailTemplateTextDeterministic(t *testing.T) {
	alert := Alert{
		ID: "alert-1", RuleID: "budget-high", Severity: SeverityHigh, Status: StatusFiring,
		Message:     "budget at 95%",
		FiredAt:     time.Unix(100, 0).UTC(),
		WindowStart: time.Unix(90, 0).UTC(),
		WindowEnd:   time.Unix(100, 0).UTC(),
		Evidence:    map[string]string{"limit": "1000", "spent": "950"},
	}
	subject, body := EmailTemplateText(alert)
	if subject != "[LiteAIG HIGH] budget-high" {
		t.Fatalf("subject = %q", subject)
	}
	first, second := EmailTemplateText(alert)
	if first != subject || second != body {
		t.Fatalf("template not deterministic")
	}
	for _, wanted := range []string{
		"LiteAIG alert", "id: alert-1", "rule: budget-high", "severity: high",
		"status: firing", "message: budget at 95%", "fired_at: 1970-01-01T00:01:40Z",
		"window: 1970-01-01T00:01:30Z .. 1970-01-01T00:01:40Z",
		"  limit: 1000", "  spent: 950",
	} {
		if !strings.Contains(body, wanted) {
			t.Fatalf("body missing %q in:\n%s", wanted, body)
		}
	}
	if strings.Count(body, "evidence:") != 1 {
		t.Fatalf("evidence header count in:\n%s", body)
	}
}

func TestEmailNotifierSendsAndSkipsEmptyRecipient(t *testing.T) {
	sender := &recordingSender{}
	notifier := &EmailNotifier{Sender: sender, To: "ops@example.com"}
	alert := Alert{ID: "alert-1", RuleID: "rule", Severity: SeverityLow}
	if err := notifier.Notify(context.Background(), alert); err != nil {
		t.Fatal(err)
	}
	if sender.to != "ops@example.com" || sender.subject == "" || sender.body == "" {
		t.Fatalf("send = to:%q subject:%q body:%q", sender.to, sender.subject, sender.body)
	}

	unchanged := *sender
	empty := &EmailNotifier{Sender: sender, To: ""}
	if err := empty.Notify(context.Background(), alert); err != nil {
		t.Fatal(err)
	}
	if *sender != unchanged {
		t.Fatalf("empty recipient must not send: %+v", sender)
	}
}

var _ mail.Sender = (*recordingSender)(nil)
