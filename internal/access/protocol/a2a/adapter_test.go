package a2a

import "testing"

func TestNormalizeAgentCardYieldsCandidateOnly(t *testing.T) {
	raw := []byte(`{"name":"partner-agent","url":"https://partner.example","capabilities":["invoice.read"]}`)
	request, card, err := NormalizeAgentCard(raw)
	if err != nil || request == nil || card.Name != "partner-agent" {
		t.Fatalf("NormalizeAgentCard() = %+v, %+v, %v", request, card, err)
	}
	// Discovery yields a candidate; trust is established elsewhere. An
	// unsigned card parses with a nil Signature, never a signature requirement.
	if card.Signature != nil {
		t.Fatal("discovery must not imply a signature requirement for trust")
	}
	if _, _, err := NormalizeAgentCard([]byte(`{"url":"https://missing-name.example"}`)); err == nil {
		t.Fatal("agent card without a name must be rejected")
	}
}

func TestNormalizeTaskMessage(t *testing.T) {
	raw := []byte(`{"messageId":"m1","role":"user","parts":[{"kind":"text","text":"process this"}]}`)
	request, err := NormalizeTaskMessage(raw)
	if err != nil || request.Chat == nil || request.Chat.Messages[0].Content != "process this" {
		t.Fatalf("NormalizeTaskMessage() = %+v, %v", request, err)
	}
	if request.SessionID != "m1" {
		t.Fatalf("messageId not preserved on session: %+v", request)
	}
	if _, err := NormalizeTaskMessage([]byte(`{"role":"user","parts":[]}`)); err == nil {
		t.Fatal("message without id must be rejected")
	}
}

func TestNormalizeTaskMessageConcatenatesTextParts(t *testing.T) {
	request, err := NormalizeTaskMessage([]byte(`{"messageId":"m2","role":"agent","parts":[{"kind":"text","text":"first"},{"kind":"text","text":"second"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := request.Chat.Messages[0].Content; got != "first\nsecond" {
		t.Fatalf("content = %q, want first\\nsecond", got)
	}
	// A2A role "agent" maps to the canonical assistant role.
	if got := request.Chat.Messages[0].Role; got != "assistant" {
		t.Fatalf("role = %q, want assistant", got)
	}
}

func TestNormalizeTaskMessageDefaultsUserRole(t *testing.T) {
	request, err := NormalizeTaskMessage([]byte(`{"messageId":"m3","parts":[{"kind":"text","text":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := request.Chat.Messages[0].Role; got != "user" {
		t.Fatalf("role = %q, want user", got)
	}
}

func TestNormalizeTaskMessageAcceptsV1Wire(t *testing.T) {
	// Official SDK v1.x sends protobuf-JSON: ROLE_* enum names and bare text
	// parts without the 0.3 `kind` discriminator.
	raw := []byte(`{"messageId":"m4","role":"ROLE_USER","parts":[{"text":"hello 1.0"}]}`)
	request, err := NormalizeTaskMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := request.Chat.Messages[0].Role; got != "user" {
		t.Fatalf("role = %q, want user", got)
	}
	if got := request.Chat.Messages[0].Content; got != "hello 1.0" {
		t.Fatalf("content = %q, want hello 1.0", got)
	}
	agent, err := NormalizeTaskMessage([]byte(`{"messageId":"m5","role":"ROLE_AGENT","parts":[{"text":"reply"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := agent.Chat.Messages[0].Role; got != "assistant" {
		t.Fatalf("ROLE_AGENT role = %q, want assistant", got)
	}
	// Non-text 1.0 part kinds (raw/url/data) stay excluded from the text relay.
	skipped, err := NormalizeTaskMessage([]byte(`{"messageId":"m6","role":"ROLE_USER","parts":[{"url":"https://x/y.png"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := skipped.Chat.Messages[0].Content; got != "" {
		t.Fatalf("content = %q, want empty (non-text part skipped)", got)
	}
}
