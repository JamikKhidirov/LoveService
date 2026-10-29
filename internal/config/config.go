// Package config loads and validates application configuration from the
// environment. All values have safe local-development defaults, except secrets
// that are mandatory in production.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Env           string
	HTTPAddr      string
	PublicBaseURL string
	LogLevel      string

	DatabaseURL string

	RedisAddr     string
	RedisPassword string
	RedisDB       int

	KafkaEnabled bool
	KafkaBrokers []string
	KafkaTopic   string
	KafkaGroupID string

	JWTSecret  string
	AccessTTL  time.Duration
	RefreshTTL time.Duration

	MediaDir         string
	MediaBaseURL     string
	StorageDriver    string
	MaxUploadMB      int64
	AutoApproveMedia bool
	DocsDir          string

	S3Endpoint  string
	S3AccessKey string
	S3SecretKey string
	S3Bucket    string
	S3Region    string
	S3UseSSL    bool
	S3PublicURL string

	CORSOrigins        []string
	RateLimitPerMinute int
	SeedDemo           bool

	DailyLikeLimit      int
	DailySuperlikeLimit int
	RewindWindow        time.Duration

	AdminRequireSecondApproval bool

	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string
	SMTPFrom     string
	SMTPTLS      string
	SMTPTimeout  time.Duration
	MailOutbox   string
}

func Load() (*Config, error) {
	// .env is optional; real environments usually inject variables directly.
	_ = godotenv.Load()

	cfg := &Config{
		Env:                        env("APP_ENV", "development"),
		HTTPAddr:                   env("HTTP_ADDR", ":8080"),
		PublicBaseURL:              strings.TrimRight(env("PUBLIC_BASE_URL", "http://localhost:8080"), "/"),
		LogLevel:                   env("LOG_LEVEL", "info"),
		DatabaseURL:                env("DATABASE_URL", "postgres://love:love@localhost:5432/love?sslmode=disable"),
		RedisAddr:                  env("REDIS_ADDR", "localhost:6379"),
		RedisPassword:              env("REDIS_PASSWORD", ""),
		RedisDB:                    envInt("REDIS_DB", 0),
		KafkaEnabled:               envBool("KAFKA_ENABLED", true),
		KafkaBrokers:               splitCSV(env("KAFKA_BROKERS", "localhost:9092")),
		KafkaTopic:                 env("KAFKA_TOPIC", "love.events"),
		KafkaGroupID:               env("KAFKA_GROUP_ID", "love-notifications"),
		JWTSecret:                  env("JWT_SECRET", "dev-only-change-me-please-32-bytes-min"),
		AccessTTL:                  envDuration("ACCESS_TTL", 15*time.Minute),
		RefreshTTL:                 envDuration("REFRESH_TTL", 30*24*time.Hour),
		MediaDir:                   env("MEDIA_DIR", "./media"),
		MediaBaseURL:               strings.TrimRight(env("MEDIA_BASE_URL", "http://localhost:8080/media"), "/"),
		StorageDriver:              env("STORAGE_DRIVER", "local"),
		MaxUploadMB:                int64(envInt("MAX_UPLOAD_MB", 50)),
		AutoApproveMedia:           envBool("AUTO_APPROVE_MEDIA", true),
		DocsDir:                    env("DOCS_DIR", "./docs"),
		S3Endpoint:                 env("S3_ENDPOINT", "localhost:9000"),
		S3AccessKey:                env("S3_ACCESS_KEY", "minioadmin"),
		S3SecretKey:                env("S3_SECRET_KEY", "minioadmin"),
		S3Bucket:                   env("S3_BUCKET", "love-media"),
		S3Region:                   env("S3_REGION", "us-east-1"),
		S3UseSSL:                   envBool("S3_USE_SSL", false),
		S3PublicURL:                strings.TrimRight(env("S3_PUBLIC_URL", ""), "/"),
		CORSOrigins:                splitCSV(env("CORS_ORIGINS", "*")),
		RateLimitPerMinute:         envInt("RATE_LIMIT_PER_MINUTE", 240),
		SeedDemo:                   envBool("SEED_DEMO", true),
		DailyLikeLimit:             envInt("DAILY_LIKE_LIMIT", 100),
		DailySuperlikeLimit:        envInt("DAILY_SUPERLIKE_LIMIT", 5),
		RewindWindow:               envDuration("REWIND_WINDOW", 5*time.Minute),
		AdminRequireSecondApproval: envBool("ADMIN_REQUIRE_SECOND_APPROVAL", true),
		SMTPHost:                   env("SMTP_HOST", ""),
		SMTPPort:                   envInt("SMTP_PORT", 587),
		SMTPUser:                   env("SMTP_USER", ""),
		SMTPPassword:               env("SMTP_PASSWORD", ""),
		SMTPFrom:                   env("SMTP_FROM", "LoveService <no-reply@loveservice.local>"),
		SMTPTLS:                    env("SMTP_TLS", "starttls"),
		SMTPTimeout:                envDuration("SMTP_TIMEOUT", 15*time.Second),
		MailOutbox:                 env("MAIL_OUTBOX_DIR", ""),
	}

	if cfg.Env == "production" {
		if len(cfg.JWTSecret) < 32 || strings.Contains(cfg.JWTSecret, "dev-only") {
			return nil, fmt.Errorf("JWT_SECRET must be set to a random value of at least 32 characters in production")
		}
		if len(cfg.CORSOrigins) == 1 && cfg.CORSOrigins[0] == "*" {
			return nil, fmt.Errorf("CORS_ORIGINS must list explicit origins in production")
		}
	}
	if cfg.MaxUploadMB < 1 || cfg.MaxUploadMB > 1024 {
		return nil, fmt.Errorf("MAX_UPLOAD_MB must be between 1 and 1024")
	}
	if cfg.AccessTTL <= 0 || cfg.RefreshTTL <= 0 {
		return nil, fmt.Errorf("token TTLs must be positive")
	}
	cfg.MediaDir = strings.TrimRight(cfg.MediaDir, "/\\")
	return cfg, nil
}

func (c *Config) IsProduction() bool { return c.Env == "production" }

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func envInt(key string, fallback int) int {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return v
}

func envBool(key string, fallback bool) bool {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback
	}
	v, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return v
}

func envDuration(key string, fallback time.Duration) time.Duration {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback
	}
	v, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return v
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
