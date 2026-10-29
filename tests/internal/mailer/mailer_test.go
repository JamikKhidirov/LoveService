// Package mailer_test contains black-box tests for internal/mailer.
package mailer_test

import (
	"context"
	"log/slog"
	"testing"

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
