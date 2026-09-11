// Package mail sends the small set of transactional emails the Lite profile
// needs (password reset codes). It is a platform capability built on the
// standard library SMTP client: explicit STARTTLS, implicit TLS, or no TLS
// (for local relays), with a plain-text MIME body.
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// TLSMode selects the SMTP transport security.
const (
	TLSStartTLS = "starttls" // plain connect, upgrade with STARTTLS (port 587)
	TLSImplicit = "implicit" // TLS from the first byte (port 465)
	TLSNone     = "none"     // no transport security (local relay only)
)

// Config is one SMTP relay.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	TLSMode  string // TLSStartTLS (default), TLSImplicit, TLSNone
}

// Sender delivers one email.
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// SMTPSender sends over one configured relay.
type SMTPSender struct {
	cfg Config
}

// NewSMTPSender validates the configuration and builds a sender.
func NewSMTPSender(cfg Config) (*SMTPSender, error) {
	if cfg.Host == "" || cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, errors.New("mail: host and port are required")
	}
	if cfg.From == "" {
		return nil, errors.New("mail: from address is required")
	}
	if _, err := mail.ParseAddress(cfg.From); err != nil {
		return nil, fmt.Errorf("mail: invalid from address: %w", err)
	}
	switch cfg.TLSMode {
	case "", TLSStartTLS:
		cfg.TLSMode = TLSStartTLS
	case TLSImplicit, TLSNone:
	default:
		return nil, fmt.Errorf("mail: unknown tls mode %q", cfg.TLSMode)
	}
	return &SMTPSender{cfg: cfg}, nil
}

// Send delivers a plain-text email to one recipient.
func (s *SMTPSender) Send(ctx context.Context, to, subject, body string) error {
	if _, err := mail.ParseAddress(to); err != nil {
		return fmt.Errorf("mail: invalid recipient: %w", err)
	}
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	dialer := net.Dialer{Timeout: 10 * time.Second}

	var client *smtp.Client
	if s.cfg.TLSMode == TLSImplicit {
		conn, err := tls.DialWithDialer(&dialer, "tcp", addr, &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12})
		if err != nil {
			return fmt.Errorf("mail: tls dial: %w", err)
		}
		client, err = smtp.NewClient(conn, s.cfg.Host)
		if err != nil {
			_ = conn.Close()
			return fmt.Errorf("mail: smtp client: %w", err)
		}
	} else {
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return fmt.Errorf("mail: dial: %w", err)
		}
		if deadline, ok := ctx.Deadline(); ok {
			_ = conn.SetDeadline(deadline)
		}
		client, err = smtp.NewClient(conn, s.cfg.Host)
		if err != nil {
			_ = conn.Close()
			return fmt.Errorf("mail: smtp client: %w", err)
		}
		if s.cfg.TLSMode == TLSStartTLS {
			if err := client.StartTLS(&tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
				_ = client.Close()
				return fmt.Errorf("mail: starttls: %w", err)
			}
		}
	}
	defer client.Close()

	if s.cfg.Username != "" {
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("mail: auth: %w", err)
		}
	}
	if err := client.Mail(s.cfg.From); err != nil {
		return fmt.Errorf("mail: mail from: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("mail: rcpt to: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("mail: data: %w", err)
	}
	message := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		s.cfg.From, to, subject, strings.ReplaceAll(body, "\n", "\r\n"))
	if _, err := writer.Write([]byte(message)); err != nil {
		return fmt.Errorf("mail: write: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("mail: close data: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("mail: quit: %w", err)
	}
	return nil
}
