// Package mailer sends transactional email. In development it logs the message
// (including verification codes) instead of contacting an SMTP server.
package mailer

import (
	"context"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
)

type Mailer interface {
	Send(ctx context.Context, to, subject, htmlBody string) error
}

func New(host string, port int, user, password, from string, log *slog.Logger) Mailer {
	if strings.TrimSpace(host) == "" {
		return &LogMailer{log: log}
	}
	return &SMTPMailer{
		addr:     fmt.Sprintf("%s:%d", host, port),
		host:     host,
		user:     user,
		password: password,
		from:     from,
		log:      log,
	}
}

type LogMailer struct {
	log *slog.Logger
}

func (m *LogMailer) Send(_ context.Context, to, subject, body string) error {
	m.log.Info("email (dev mode, not sent)", "to", to, "subject", subject, "body", body)
	return nil
}

type SMTPMailer struct {
	addr     string
	host     string
	user     string
	password string
	from     string
	log      *slog.Logger
}

func (m *SMTPMailer) Send(_ context.Context, to, subject, body string) error {
	headers := []string{
		"From: " + m.from,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
	}
	msg := []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + body)
	var auth smtp.Auth
	if m.user != "" {
		auth = smtp.PlainAuth("", m.user, m.password, m.host)
	}
	if err := smtp.SendMail(m.addr, auth, m.fromAddress(), []string{to}, msg); err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	return nil
}

func (m *SMTPMailer) fromAddress() string {
	if i := strings.LastIndex(m.from, "<"); i >= 0 {
		return strings.TrimSuffix(strings.TrimSpace(m.from[i+1:]), ">")
	}
	return m.from
}
