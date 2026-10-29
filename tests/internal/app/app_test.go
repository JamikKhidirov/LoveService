// Package app_test contains black-box tests for internal/app.
package app_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/loveservice/loveservice/internal/app"
	"github.com/loveservice/loveservice/internal/config"
)

func TestNewFailsFastOnInvalidDatabaseURL(t *testing.T) {
	cfg := &config.Config{DatabaseURL: "://not-a-dsn", Env: "development"}
	_, err := app.New(context.Background(), cfg, slog.Default(), "test")
	if err == nil {
		t.Fatal("expected invalid database URL to fail")
	}
}
