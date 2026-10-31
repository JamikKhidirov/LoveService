//go:build integration

package integration_test

import (
	"net/http"
	"testing"
)

// TestMonetizationSessionsAndGifts exercises the v1.2 surface end to end:
// device sessions, the "who liked me" paywall, coins, discovery ranking,
// profile statistics, top picks and virtual gifts.
func TestMonetizationSessionsAndGifts(t *testing.T) {
	token, _ := login(t, "demo@loveservice.app", "LoveDemo123!")

	// Device sessions: the device that made this request must be marked.
	status, body, raw := request(t, http.MethodGet, "/api/v1/sessions", token, nil)
	if status != http.StatusOK {
		t.Fatalf("list sessions: %d %s", status, raw)
	}
	current := false
	for _, item := range dataItems(t, body) {
		if item["current"] == true {
			current = true
			if _, ok := item["created_at"].(string); !ok {
				t.Fatalf("session has no login date: %v", item)
			}
		}
	}
	if !current {
		t.Fatalf("no session is marked as current: %s", raw)
	}

	// Premium status and the mock coin top-up.
	status, body, raw = request(t, http.MethodGet, "/api/v1/premium", token, nil)
	if status != http.StatusOK {
		t.Fatalf("premium status: %d %s", status, raw)
	}
	if dataMap(t, body)["unlock_cost_coins"] != float64(100) {
		t.Fatalf("unexpected unlock price: %s", raw)
	}
	status, body, raw = request(t, http.MethodPost, "/api/v1/wallet/topup", token, map[string]any{
		"coins": 500, "reference": "integration-test",
	})
	if status != http.StatusOK {
		t.Fatalf("wallet top-up: %d %s", status, raw)
	}
	if coins, _ := dataMap(t, body)["coins"].(float64); coins < 500 {
		t.Fatalf("wallet was not credited: %s", raw)
	}

	// Free users see only a counter; the one-off unlock reveals the list.
	status, body, raw = request(t, http.MethodGet, "/api/v1/discovery/likes", token, nil)
	if status != http.StatusOK {
		t.Fatalf("incoming likes: %d %s", status, raw)
	}
	likes := dataMap(t, body)
	if _, ok := likes["premium_required"].(bool); !ok {
		t.Fatalf("incoming likes lack the paywall flag: %s", raw)
	}
	if likes["premium_required"] == true {
		if items, ok := likes["items"].([]any); !ok || len(items) != 0 {
			t.Fatalf("free users must not receive profiles: %s", raw)
		}
	}
	status, _, raw = request(t, http.MethodPost, "/api/v1/discovery/likes/unlock", token, nil)
	if status != http.StatusOK {
		t.Fatalf("unlock likes: %d %s", status, raw)
	}
	status, body, raw = request(t, http.MethodGet, "/api/v1/discovery/likes", token, nil)
	if status != http.StatusOK {
		t.Fatalf("incoming likes after unlock: %d %s", status, raw)
	}
	if dataMap(t, body)["premium_required"] != false {
		t.Fatalf("likes are still locked after unlock: %s", raw)
	}

	// Every ranking mode must be accepted by the SQL layer.
	for _, sortMode := range []string{"balanced", "distance", "rating"} {
		status, _, raw = request(t, http.MethodGet,
			"/api/v1/discovery?lat=55.7558&lng=37.6173&radius_km=3000&expand=false&limit=5&sort="+sortMode, token, nil)
		if status != http.StatusOK {
			t.Fatalf("discovery sort=%s: %d %s", sortMode, status, raw)
		}
	}
	if status, _, raw := request(t, http.MethodGet, "/api/v1/discovery?sort=magic", token, nil); status != http.StatusUnprocessableEntity {
		t.Fatalf("unknown sort must be rejected, got %d %s", status, raw)
	}

	// Insights.
	for _, path := range []string{"/api/v1/profile/stats", "/api/v1/recommendations/top-picks", "/api/v1/gifts/catalog"} {
		status, _, raw = request(t, http.MethodGet, path, token, nil)
		if status != http.StatusOK {
			t.Fatalf("%s: %d %s", path, status, raw)
		}
	}

	// A gift moves coins atomically and appears as a gift message.
	status, body, raw = request(t, http.MethodGet, "/api/v1/conversations", token, nil)
	if status != http.StatusOK {
		t.Fatalf("conversations: %d %s", status, raw)
	}
	conversations := dataItems(t, body)
	if len(conversations) == 0 {
		t.Skip("demo account has no conversation to send a gift in")
	}
	conversationID, _ := conversations[0]["id"].(string)
	status, body, raw = request(t, http.MethodPost, "/api/v1/chat/"+conversationID+"/gifts", token, map[string]string{
		"gift_code": "rose",
	})
	if status != http.StatusCreated {
		t.Fatalf("send gift: %d %s", status, raw)
	}
	gift := dataMap(t, body)
	message, _ := gift["message"].(map[string]any)
	if message == nil || message["type"] != "gift" {
		t.Fatalf("gift message has the wrong type: %s", raw)
	}
	metadata, _ := message["metadata"].(map[string]any)
	if metadata["gift_code"] != "rose" {
		t.Fatalf("gift metadata is missing the code: %s", raw)
	}
	if status, _, raw := request(t, http.MethodPost, "/api/v1/chat/"+conversationID+"/gifts", token, map[string]string{
		"gift_code": "unicorn",
	}); status != http.StatusUnprocessableEntity {
		t.Fatalf("unknown gift must be rejected, got %d %s", status, raw)
	}
}
