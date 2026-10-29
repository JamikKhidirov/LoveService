// Package ws_test contains black-box tests for internal/transport/ws.
package ws_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/cache"
	"github.com/loveservice/loveservice/internal/security"
	"github.com/loveservice/loveservice/internal/transport/ws"
)

func newHub(t *testing.T) (*ws.Hub, *cache.Cache) {
	t.Helper()
	c := cache.New(context.Background(), "", "", 0, slog.Default())
	t.Cleanup(func() { _ = c.Close() })
	return ws.NewHub(c, slog.Default()), c
}

func TestHubStartsEmptyAndPublishesSafely(t *testing.T) {
	hub, _ := newHub(t)
	if got := hub.OnlineUsers(); got != 0 {
		t.Fatalf("OnlineUsers = %d", got)
	}
	// Sending to a disconnected user must be a no-op, not a panic.
	hub.SendToUser(uuid.New(), "match.new", map[string]string{"match_id": "m1"})
	hub.SendToUsers([]uuid.UUID{uuid.New(), uuid.New()}, "notification.new", map[string]string{"id": "n1"})
	hub.DeliverPayload([]byte("not valid json"))
}

func TestHubPresence(t *testing.T) {
	hub, c := newHub(t)
	ctx := context.Background()
	id := uuid.New()

	hub.SetOnline(ctx, id)
	if !c.IsOnline(ctx, id) {
		t.Fatal("expected presence to be stored in the cache")
	}
	hub.RefreshOnline(ctx, id)
	hub.SetOffline(ctx, id)
	if c.IsOnline(ctx, id) {
		t.Fatal("expected presence to be removed")
	}
}

func TestHandlerRejectsMissingToken(t *testing.T) {
	hub, _ := newHub(t)
	tokens := security.NewTokenManager("01234567890123456789012345678901", 15*time.Minute, time.Hour)
	h := ws.NewHandler(hub, tokens, nil, nil, []string{"*"}, slog.Default())

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/ws", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
}

func TestHandlerRejectsInvalidToken(t *testing.T) {
	hub, _ := newHub(t)
	tokens := security.NewTokenManager("01234567890123456789012345678901", 15*time.Minute, time.Hour)
	h := ws.NewHandler(hub, tokens, nil, nil, []string{"*"}, slog.Default())

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/ws?token=broken", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
}
