// Package mailer sends transactional email straight from the service over
// SMTP. No external SaaS API is required: the service generates the code,
// renders the HTML letter and delivers it through the configured mailbox.
//
// In development (no SMTP_HOST) messages are logged and, when an outbox
// directory is configured, written as .eml files so the exact letter can be
// opened in a browser.
package mailer

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log/slog"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Mailer interface {
	// Send delivers an HTML message. The plain-text part is derived from the
	// markup automatically; prefer SendMessage when you have both variants.
	Send(ctx context.Context, to, subject, htmlBody string) error
	// SendMessage delivers a fully rendered message.
	SendMessage(ctx context.Context, to string, msg Message) error
}

// Config describes the SMTP transport.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	From     string
	// TLSMode is "starttls" (default, port 587), "tls" (implicit TLS, port 465)
	// or "none" (plain SMTP, only for a local relay or tests).
	TLSMode string
	// Timeout bounds a single delivery attempt. Defaults to 15s.
	Timeout time.Duration
	// OutboxDir makes the development mailer write .eml files for preview.
	OutboxDir string
}

// New keeps the original constructor: an empty host selects the development
// logger, everything else uses STARTTLS (or implicit TLS on port 465).
func New(host string, port int, user, password, from string, log *slog.Logger) Mailer {
	return NewWithConfig(Config{
		Host: host, Port: port, User: user, Password: password, From: from,
	}, log)
}

// NewWithConfig builds the production SMTP mailer or the development logger.
func NewWithConfig(cfg Config, log *slog.Logger) Mailer {
	if log == nil {
		log = slog.Default()
	}
	if strings.TrimSpace(cfg.Host) == "" {
		return &LogMailer{log: log, outboxDir: cfg.OutboxDir}
	}
	if cfg.Port == 0 {
		cfg.Port = 587
	}
	if cfg.From == "" {
		cfg.From = "LoveService <no-reply@loveservice.local>"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}
	return &SMTPMailer{
		addr:     net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port)),
		host:     cfg.Host,
		port:     cfg.Port,
		user:     cfg.User,
		password: cfg.Password,
		from:     cfg.From,
		tlsMode:  strings.ToLower(strings.TrimSpace(cfg.TLSMode)),
		timeout:  cfg.Timeout,
		log:      log,
	}
}

type LogMailer struct {
	log       *slog.Logger
	outboxDir string
}

func (m *LogMailer) Send(ctx context.Context, to, subject, htmlBody string) error {
	return m.SendMessage(ctx, to, Message{Subject: subject, HTML: htmlBody})
}

func (m *LogMailer) SendMessage(_ context.Context, to string, msg Message) error {
	raw, err := buildMIME("LoveService <no-reply@loveservice.local>", to, msg.Subject, msg.HTML, msg.Text)
	if err != nil {
		return err
	}
	if m.outboxDir != "" {
		if err := writeOutbox(m.outboxDir, to, raw); err != nil {
			m.log.Warn("could not write development email to outbox", "error", err)
		} else {
			m.log.Info("email written to development outbox", "to", to, "subject", msg.Subject, "dir", m.outboxDir)
			return nil
		}
	}
	m.log.Info("email (dev mode, not sent)", "to", to, "subject", msg.Subject, "body", msg.Text)
	return nil
}

type SMTPMailer struct {
	addr     string
	host     string
	port     int
	user     string
	password string
	from     string
	tlsMode  string
	timeout  time.Duration
	log      *slog.Logger
}

func (m *SMTPMailer) Send(ctx context.Context, to, subject, htmlBody string) error {
	return m.SendMessage(ctx, to, Message{Subject: subject, HTML: htmlBody})
}

func (m *SMTPMailer) SendMessage(ctx context.Context, to string, msg Message) error {
	raw, err := buildMIME(m.from, to, msg.Subject, msg.HTML, msg.Text)
	if err != nil {
		return err
	}
	from, err := mailboxOf(m.from)
	if err != nil {
		return fmt.Errorf("invalid sender address: %w", err)
	}

	// Three attempts smooth over transient network/relay hiccups. The caller
	// runs delivery in the background, so this never blocks a response.
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := m.deliver(ctx, from, to, raw); err != nil {
			lastErr = err
			m.log.Warn("smtp delivery attempt failed", "to", to, "attempt", attempt, "error", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 400 * time.Millisecond):
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("send email after 3 attempts: %w", lastErr)
}

func (m *SMTPMailer) deliver(ctx context.Context, from, to string, raw []byte) error {
	deadline := time.Now().Add(m.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	dialer := &net.Dialer{Timeout: m.timeout, Deadline: deadline}
	conn, err := dialer.DialContext(ctx, "tcp", m.addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)

	implicitTLS := m.tlsMode == "tls" || m.tlsMode == "ssl" || (m.tlsMode == "" && m.port == 465)
	if implicitTLS {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: m.host, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			return fmt.Errorf("tls handshake: %w", err)
		}
		conn = tlsConn
	}

	client, err := smtp.NewClient(conn, m.host)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if !implicitTLS && m.tlsMode != "none" {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: m.host, MinVersion: tls.VersionTLS12}); err != nil {
				return fmt.Errorf("starttls: %w", err)
			}
		} else if m.tlsMode == "starttls" {
			return fmt.Errorf("smtp server does not support STARTTLS")
		}
	}

	if m.user != "" {
		auth := smtp.PlainAuth("", m.user, m.password, m.host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	wc, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := wc.Write(raw); err != nil {
		_ = wc.Close()
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func mailboxOf(addr string) (string, error) {
	parsed, err := mail.ParseAddress(addr)
	if err != nil {
		return "", err
	}
	return parsed.Address, nil
}

// buildMIME renders a multipart/alternative message with a plain-text and an
// HTML part. Subject and body are UTF-8 and RFC 2047/2045 encoded so Cyrillic
// arrives intact in every client.
func buildMIME(from, to, subject, htmlBody, textBody string) ([]byte, error) {
	fromAddr, err := mail.ParseAddress(sanitizeHeader(from))
	if err != nil {
		return nil, fmt.Errorf("invalid from address: %w", err)
	}
	toAddr, err := mail.ParseAddress(sanitizeHeader(to))
	if err != nil {
		return nil, fmt.Errorf("invalid recipient address: %w", err)
	}
	if strings.TrimSpace(subject) == "" {
		subject = "LoveService"
	}
	if strings.TrimSpace(htmlBody) == "" {
		htmlBody = "<p>" + htmlEscape(textBody) + "</p>"
	}
	if strings.TrimSpace(textBody) == "" {
		textBody = htmlToText(htmlBody)
	}

	boundary, err := randomBoundary()
	if err != nil {
		return nil, err
	}
	domain := "loveservice.local"
	if at := strings.LastIndex(fromAddr.Address, "@"); at >= 0 {
		domain = fromAddr.Address[at+1:]
	}

	var b strings.Builder
	b.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("From: " + fromAddr.String() + "\r\n")
	b.WriteString("To: " + toAddr.String() + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	b.WriteString("Message-ID: <" + uuid.NewString() + "@" + domain + ">\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("X-Mailer: LoveService\r\n")
	b.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n")
	b.WriteString("\r\n")

	writePart := func(contentType, body string) error {
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: " + contentType + "; charset=\"utf-8\"\r\n")
		b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		qp := quotedprintable.NewWriter(&b)
		if _, err := qp.Write([]byte(normalizeNewlines(body))); err != nil {
			return err
		}
		if err := qp.Close(); err != nil {
			return err
		}
		b.WriteString("\r\n")
		return nil
	}
	if err := writePart("text/plain", textBody); err != nil {
		return nil, err
	}
	if err := writePart("text/html", htmlBody); err != nil {
		return nil, err
	}
	b.WriteString("--" + boundary + "--\r\n")

	if len(b.String()) > 10<<20 {
		return nil, fmt.Errorf("email body exceeds 10 MiB")
	}
	return []byte(b.String()), nil
}

func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

func randomBoundary() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "love-" + hex.EncodeToString(buf), nil
}

func sanitizeHeader(v string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(strings.TrimSpace(v))
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;").Replace(s)
}

func writeOutbox(dir, to string, raw []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("%s-%s.eml", time.Now().UTC().Format("20060102-150405.000"), sanitizeFilename(to))
	return os.WriteFile(filepath.Join(dir, name), raw, 0o644)
}

func sanitizeFilename(s string) string {
	mapped := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_', r == '@':
			return r
		default:
			return '-'
		}
	}, s)
	if len(mapped) > 80 {
		mapped = mapped[:80]
	}
	if mapped == "" {
		return "message"
	}
	return mapped
}
