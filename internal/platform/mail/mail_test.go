package mail

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeSMTP is a minimal SMTP relay capturing the DATA payload.
type fakeSMTP struct {
	mu       sync.Mutex
	listener net.Listener
	data     string
	commands []string
	closed   bool
}

func startFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeSMTP{listener: listener}
	go fake.serve()
	t.Cleanup(func() { _ = listener.Close() })
	return fake
}

func (f *fakeSMTP) serve() {
	conn, err := f.listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = conn.Write([]byte("220 fake ESMTP\r\n"))
	reader := bufio.NewReader(conn)
	dataMode := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		trimmed := strings.TrimSpace(line)
		f.mu.Lock()
		f.commands = append(f.commands, trimmed)
		f.mu.Unlock()
		switch {
		case trimmed == "QUIT":
			_, _ = conn.Write([]byte("221 bye\r\n"))
			return
		case trimmed == "EHLO fake":
			_, _ = conn.Write([]byte("250-fake\r\n250 OK\r\n"))
		case strings.HasPrefix(trimmed, "EHLO"):
			_, _ = conn.Write([]byte("250 fake\r\n"))
		case strings.HasPrefix(trimmed, "MAIL FROM:"):
			_, _ = conn.Write([]byte("250 ok\r\n"))
		case strings.HasPrefix(trimmed, "RCPT TO:"):
			_, _ = conn.Write([]byte("250 ok\r\n"))
		case trimmed == "DATA":
			_, _ = conn.Write([]byte("354 go ahead\r\n"))
			dataMode = true
		case dataMode && trimmed == ".":
			dataMode = false
			_, _ = conn.Write([]byte("250 queued\r\n"))
		case dataMode:
			f.mu.Lock()
			f.data += line
			f.mu.Unlock()
		default:
			_, _ = conn.Write([]byte("250 ok\r\n"))
		}
	}
}

func TestSMTPSendDeliversMessage(t *testing.T) {
	fake := startFakeSMTP(t)
	cfg := Config{Host: "127.0.0.1", Port: portOf(t, fake), From: "no-reply@example.com", TLSMode: TLSNone}
	sender, err := NewSMTPSender(cfg)
	if err != nil {
		t.Fatal(err)
	}
	err = sender.Send(context.Background(), "alice@example.com", "Reset", "Your code is 123456\nSecond line")
	if err != nil {
		t.Fatalf("Send() = %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if !strings.Contains(fake.data, "Subject: Reset") || !strings.Contains(fake.data, "Your code is 123456") {
		t.Fatalf("payload = %q", fake.data)
	}
	if !strings.Contains(fake.data, "alice@example.com") || !strings.Contains(fake.data, "no-reply@example.com") {
		t.Fatalf("addresses missing from payload = %q", fake.data)
	}
	var sawMail, sawRcpt bool
	for _, command := range fake.commands {
		if strings.HasPrefix(command, "MAIL FROM:") {
			sawMail = true
		}
		if strings.HasPrefix(command, "RCPT TO:") {
			sawRcpt = true
		}
	}
	if !sawMail || !sawRcpt {
		t.Fatalf("commands = %v; expected MAIL FROM and RCPT TO", fake.commands)
	}
}

func TestSMTPSendRejectsBadRecipient(t *testing.T) {
	sender, err := NewSMTPSender(Config{Host: "127.0.0.1", Port: 25, From: "no-reply@example.com", TLSMode: TLSNone})
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.Send(context.Background(), "not-an-address", "s", "b"); err == nil {
		t.Fatal("invalid recipient accepted")
	}
}

func TestNewSMTPSenderValidatesConfig(t *testing.T) {
	base := Config{Host: "smtp.example.com", Port: 587, From: "no-reply@example.com"}
	for _, broken := range []Config{
		{Port: base.Port, From: base.From},                                    // no host
		{Host: base.Host, From: base.From},                                    // no port
		{Host: base.Host, Port: 99999, From: base.From},                       // bad port
		{Host: base.Host, Port: base.Port},                                    // no from
		{Host: base.Host, Port: base.Port, From: "not-an-address"},            // bad from
		{Host: base.Host, Port: base.Port, From: base.From, TLSMode: "bogus"}, // bad tls mode
	} {
		if _, err := NewSMTPSender(broken); err == nil {
			t.Fatalf("NewSMTPSender(%+v) accepted invalid config", broken)
		}
	}
	if _, err := NewSMTPSender(Config{Host: base.Host, Port: 465, From: base.From, TLSMode: TLSImplicit}); err != nil {
		t.Fatalf("implicit tls config rejected: %v", err)
	}
}

func portOf(t *testing.T, fake *fakeSMTP) int {
	t.Helper()
	_, portString, err := net.SplitHostPort(fake.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		t.Fatal(err)
	}
	return port
}
