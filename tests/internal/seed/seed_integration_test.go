//go:build integration

package seed_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/loveservice/loveservice/internal/seed"
	"github.com/loveservice/loveservice/internal/storage"
	"github.com/loveservice/loveservice/internal/store"
)

func TestSeedCreatesDemoAndStaff(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://love:love@localhost:5432/love?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	st, err := store.New(ctx, dsn, slog.Default())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer st.Close()

	media, err := storage.NewLocal(t.TempDir(), "http://localhost:8080/media")
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Run(ctx, st, media, slog.Default()); err != nil {
		t.Fatalf("seed.Run: %v", err)
	}

	for _, email := range []string{"demo@loveservice.app", seed.AdminEmail, seed.ModEmail} {
		if _, err := st.GetUserByEmail(ctx, email); err != nil {
			t.Fatalf("seeded user %s missing: %v", email, err)
		}
	}
}
