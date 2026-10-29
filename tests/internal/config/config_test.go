// Package config_test contains black-box tests for internal/config.
package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/loveservice/loveservice/internal/config"
)

func TestLoadReadsEnvironment(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":9090")
	t.Setenv("ACCESS_TTL", "5m")
	t.Setenv("KAFKA_ENABLED", "false")
	t.Setenv("DAILY_LIKE_LIMIT", "7")
	t.Setenv("CORS_ORIGINS", "https://a.example, https://b.example")
	t.Setenv("ADMIN_REQUIRE_SECOND_APPROVAL", "false")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != ":9090" {
		t.Fatalf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.AccessTTL != 5*time.Minute {
		t.Fatalf("AccessTTL = %v", cfg.AccessTTL)
	}
	if cfg.KafkaEnabled {
		t.Fatal("KafkaEnabled should be false")
	}
	if cfg.DailyLikeLimit != 7 {
		t.Fatalf("DailyLikeLimit = %d", cfg.DailyLikeLimit)
	}
	if len(cfg.CORSOrigins) != 2 || cfg.CORSOrigins[0] != "https://a.example" {
		t.Fatalf("CORSOrigins = %v", cfg.CORSOrigins)
	}
	if cfg.AdminRequireSecondApproval {
		t.Fatal("AdminRequireSecondApproval should be false")
	}
}

func TestLoadRejectsWeakProductionSecret(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", "dev-only-change-me-please-32-bytes-min")
	t.Setenv("CORS_ORIGINS", "https://app.example")

	if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("expected JWT_SECRET error, got %v", err)
	}
}

func TestLoadRejectsWildcardCORSInProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", strings.Repeat("x", 48))
	t.Setenv("CORS_ORIGINS", "*")

	if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "CORS_ORIGINS") {
		t.Fatalf("expected CORS_ORIGINS error, got %v", err)
	}
}

func TestLoadAcceptsProductionWithExplicitConfig(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", strings.Repeat("x", 48))
	t.Setenv("CORS_ORIGINS", "https://app.example")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.IsProduction() {
		t.Fatal("expected production mode")
	}
}

func TestInvalidEnvironmentValuesFallBack(t *testing.T) {
	t.Setenv("MAX_UPLOAD_MB", "not-a-number")
	t.Setenv("KAFKA_ENABLED", "maybe")
	t.Setenv("REWIND_WINDOW", "soon")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MaxUploadMB != 50 {
		t.Fatalf("MaxUploadMB fallback = %d", cfg.MaxUploadMB)
	}
	if !cfg.KafkaEnabled {
		t.Fatal("KafkaEnabled fallback should be true")
	}
	if cfg.RewindWindow != 5*time.Minute {
		t.Fatalf("RewindWindow fallback = %v", cfg.RewindWindow)
	}
}
