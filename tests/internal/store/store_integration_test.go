//go:build integration

package store_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/loveservice/loveservice/internal/store"
)

func TestMigrationsAndPing(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://love:love@localhost:5432/love?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st, err := store.New(ctx, dsn, slog.Default())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer st.Close()

	if err := st.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
}
