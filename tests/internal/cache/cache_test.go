// Package cache_test contains black-box tests for internal/cache. Redis is not
// required: the package must degrade to in-memory mode.
package cache_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/cache"
)

func newMemoryCache(t *testing.T) *cache.Cache {
	t.Helper()
	c := cache.New(context.Background(), "", "", 0, slog.Default())
	if c.Available() {
		t.Fatal("expected degraded in-memory cache without redis address")
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestIncrAndCount(t *testing.T) {
	c := newMemoryCache(t)
	ctx := context.Background()

	first, err := c.Incr(ctx, "test:counter", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.Incr(ctx, "test:counter", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if first != 1 || second != 2 {
		t.Fatalf("Incr = %d, %d", first, second)
	}
	if got := c.Count(ctx, "test:counter"); got != 2 {
		t.Fatalf("Count = %d", got)
	}
}

func TestAllowWindow(t *testing.T) {
	c := newMemoryCache(t)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		allowed, remaining, err := c.Allow(ctx, "test:allow", 2, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if !allowed {
			t.Fatalf("request %d should be allowed", i+1)
		}
		if remaining != 1-i {
			t.Fatalf("remaining = %d", remaining)
		}
	}
	allowed, _, err := c.Allow(ctx, "test:allow", 2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("third request must be rate limited")
	}
}

func TestPresence(t *testing.T) {
	c := newMemoryCache(t)
	ctx := context.Background()
	id := uuid.New()

	if c.IsOnline(ctx, id) {
		t.Fatal("user should be offline")
	}
	c.SetOnline(ctx, id, time.Minute)
	if !c.IsOnline(ctx, id) {
		t.Fatal("user should be online")
	}
	if online := c.OnlineMany(ctx, []uuid.UUID{id, uuid.New()}); !online[id] {
		t.Fatal("OnlineMany should report the online user")
	}
	c.SetOffline(ctx, id)
	if c.IsOnline(ctx, id) {
		t.Fatal("user should be offline after SetOffline")
	}
}

func TestJSONCache(t *testing.T) {
	c := newMemoryCache(t)
	ctx := context.Background()
	value := map[string]any{"hello": "world"}

	if err := c.SetJSON(ctx, "test:json", value, time.Minute); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	found, err := c.GetJSON(ctx, "test:json", &got)
	if err != nil {
		t.Fatal(err)
	}
	if !found || got["hello"] != "world" {
		t.Fatalf("GetJSON = %v, %v", found, got)
	}
	if found, _ := c.GetJSON(ctx, "test:missing", &got); found {
		t.Fatal("missing key must not be found")
	}
}

func TestKeys(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	at := time.Date(2026, 1, 2, 23, 30, 0, 0, time.UTC)

	if got := cache.DailyKey("likes", id, at); got != "likes:"+id.String()+":20260102" {
		t.Fatalf("DailyKey = %q", got)
	}
	if got := cache.LimitKey("like", id); got != "limit:like:"+id.String() {
		t.Fatalf("LimitKey = %q", got)
	}
	if got := cache.SecondsUntilMidnight(at); got != 30*time.Minute {
		t.Fatalf("SecondsUntilMidnight = %v", got)
	}
	if cache.Int64String(42) != "42" {
		t.Fatal("Int64String mismatch")
	}
}
