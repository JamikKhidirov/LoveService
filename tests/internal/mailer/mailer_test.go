// Package mailer_test contains black-box tests for internal/mailer.
package mailer_test

import (
	"bufio"
	"context"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/loveservice/loveservice/internal/mailer"
)

func TestNewFallsBackToLogMailer(t *testing.T) {
	m := mailer.New("", 0, "", "", "LoveService <no-reply@test>", slog.Default())
	if _, ok := m.(*mailer.LogMailer); !ok {
		t.Fatalf("expected *LogMailer, got %T", m)
	}
	if err := m.Send(context.Background(), "user@example.com", "Subject", "<b>Body</b>"); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func TestNewUsesSMTPWhenHostIsSet(t *testing.T) {
	m := mailer.New("smtp.example.com", 587, "user", "pass", "LoveService <no-reply@test>", slog.Default())
	if _, ok := m.(*mailer.SMTPMailer); !ok {
		t.Fatalf("expected *SMTPMailer, got %T", m)
	}
}

func TestTemplatesRenderBrandedRussianEmail(t *testing.T) {
	verification := mailer.VerificationEmail("482913")
	reset := mailer.ResetPasswordEmail("902144")

	for name, msg := range map[string]mailer.Message{"verification": verification, "reset": reset} {
		if !strings.Contains(msg.Subject, "LoveService") {
			t.Errorf("%s subject = %q", name, msg.Subject)
		}
		if !strings.Contains(msg.HTML, "<!DOCTYPE html>") {
			t.Errorf("%s html is not a document", name)
		}
		if !strings.Contains(msg.HTML, "LoveService") {
			t.Errorf("%s html has no brand", name)
		}
		if !strings.Contains(msg.Text, "Ваш код:") {
			t.Errorf("%s text part is missing the code label", name)
		}
	}
	if !strings.Contains(verification.HTML, "482913") || !strings.Contains(verification.Text, "482913") {
		t.Error("verification email lost the code")
	}
	if !strings.Contains(reset.HTML, "902144") || !strings.Contains(reset.Text, "902144") {
		t.Error("reset email lost the code")
	}
	if !strings.Contains(verification.HTML, "15 минут") {
		t.Error("verification email should state the code lifetime")
	}
	if !strings.Contains(verification.HTML, `lang="ru"`) {
		t.Error("email must declare the Russian locale")
	}
}

func TestLogMailerWritesEmlOutbox(t *testing.T) {
	dir := t.TempDir()
	m := mailer.NewWithConfig(mailer.Config{OutboxDir: dir}, slog.Default())
	if err := m.SendMessage(context.Background(), "user@example.com", mailer.VerificationEmail("123456")); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one outbox file, got %d (%v)", len(entries), err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	message := string(raw)
	for _, want := range []string{
		"multipart/alternative",
		"Content-Type: text/plain",
		"Content-Type: text/html",
		"Subject: ", // RFC 2047 encoded below
		"123456",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("outbox message is missing %q", want)
		}
	}
	if strings.Contains(message, "Subject: LoveService") {
		t.Error("non-ASCII subject must be RFC 2047 encoded")
	}
}

func TestSendRejectsHeaderInjection(t *testing.T) {
	m := mailer.New("", 0, "", "", "LoveService <no-reply@test>", slog.Default())
	err := m.SendMessage(context.Background(), "user@example.com\r\nBcc: attacker@example.com",
		mailer.Message{Subject: "x", HTML: "<p>x</p>"})
	if err == nil {
		t.Fatal("expected an invalid recipient error")
	}
}

func TestSMTPMailerDeliversMultipartMessage(t *testing.T) {
	addr, received, stop := startFakeSMTP(t)
	defer stop()

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	portNum, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	m := mailer.NewWithConfig(mailer.Config{
		Host: host, Port: portNum, From: "LoveService <no-reply@loveservice.test>",
		TLSMode: "none", Timeout: 5 * time.Second,
	}, slog.Default())

	if err := m.SendMessage(context.Background(), "user@example.com", mailer.VerificationEmail("777888")); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	select {
	case raw := <-received:
		message := string(raw)
		for _, want := range []string{
			"no-reply@loveservice.test",
			"user@example.com",
			"multipart/alternative",
			"Content-Type: text/plain",
			"Content-Type: text/html",
			"777888",
			"MIME-Version: 1.0",
		} {
			if !strings.Contains(message, want) {
				t.Errorf("delivered message is missing %q\n---\n%s", want, message)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SMTP server did not receive the message")
	}
}

// startFakeSMTP runs a minimal SMTP server good enough for net/smtp and returns
// its address plus a channel with the DATA payload.
func startFakeSMTP(t *testing.T) (string, <-chan []byte, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan []byte, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSMTP(conn, received)
		}
	}()
	return ln.Addr().String(), received, func() {
		_ = ln.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}
}

func serveSMTP(conn net.Conn, received chan<- []byte) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	write := func(line string) {
		_, _ = w.WriteString(line + "\r\n")
		_ = w.Flush()
	}
	write("220 fake.local ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			_, _ = w.WriteString("250-fake.local\r\n250-8BITMIME\r\n250 OK\r\n")
			_ = w.Flush()
		case strings.HasPrefix(cmd, "MAIL FROM"), strings.HasPrefix(cmd, "RCPT TO"):
			write("250 OK")
		case cmd == "DATA":
			write("354 End data with <CR><LF>.<CR><LF>")
			var body strings.Builder
			for {
				dataLine, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if dataLine == ".\r\n" {
					break
				}
				body.WriteString(dataLine)
			}
			select {
			case received <- []byte(body.String()):
			default:
			}
			write("250 OK queued")
		case cmd == "QUIT":
			write("221 Bye")
			return
		default:
			write("250 OK")
		}
	}
}
