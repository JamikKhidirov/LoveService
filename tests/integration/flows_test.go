//go:build integration

package integration_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func uniqueEmail(prefix string) string {
	return fmt.Sprintf("%s-%d@example.test", prefix, time.Now().UnixNano())
}

func register(t *testing.T, email, name, gender string, preferred []string) (token, id, devCode string) {
	t.Helper()
	status, body, raw := request(t, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":             email,
		"password":          "StrongPass123!",
		"display_name":      name,
		"birth_date":        "1996-04-12",
		"gender":            gender,
		"preferred_genders": preferred,
	})
	if status != http.StatusCreated {
		t.Fatalf("register %s: %d %s", email, status, raw)
	}
	data := dataMap(t, body)
	user, _ := data["user"].(map[string]any)
	tokens, _ := data["tokens"].(map[string]any)
	token, _ = tokens["access_token"].(string)
	id, _ = user["id"].(string)
	devCode, _ = data["dev_code"].(string)
	if token == "" || id == "" {
		t.Fatalf("register response incomplete: %s", raw)
	}
	return token, id, devCode
}

func dataItems(t *testing.T, envelope map[string]any) []map[string]any {
	t.Helper()
	raw, ok := dataMap(t, envelope)["items"].([]any)
	if !ok {
		t.Fatalf("response has no items array: %v", envelope)
	}
	items := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			items = append(items, m)
		}
	}
	return items
}

func TestRegistrationAndSessionLifecycle(t *testing.T) {
	email := uniqueEmail("lifecycle")
	token, userID, devCode := register(t, email, "Lifecycle User", "female", []string{"male"})
	if userID == "" {
		t.Fatal("missing user id")
	}

	// Verification code is returned in development so the flow is testable.
	if devCode == "" {
		status, body, raw := request(t, http.MethodPost, "/api/v1/auth/resend-verification", "", map[string]string{"email": email})
		if status != http.StatusOK {
			t.Fatalf("resend verification: %d %s", status, raw)
		}
		devCode, _ = dataMap(t, body)["dev_code"].(string)
	}
	if devCode == "" {
		t.Fatal("expected a development verification code")
	}

	if status, _, _ := request(t, http.MethodPost, "/api/v1/auth/verify-email", "", map[string]string{
		"email": email, "code": "000000",
	}); status == http.StatusOK {
		t.Fatal("wrong verification code must not be accepted")
	}

	status, body, raw := request(t, http.MethodPost, "/api/v1/auth/verify-email", "", map[string]string{
		"email": email, "code": devCode,
	})
	if status != http.StatusOK {
		t.Fatalf("verify email: %d %s", status, raw)
	}
	if verified, _ := dataMap(t, body)["verified"].(bool); !verified {
		t.Fatalf("expected verified=true: %s", raw)
	}

	status, body, raw = request(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"email": email, "password": "StrongPass123!",
	})
	if status != http.StatusOK {
		t.Fatalf("login: %d %s", status, raw)
	}
	login := dataMap(t, body)
	tokens, _ := login["tokens"].(map[string]any)
	refresh, _ := tokens["refresh_token"].(string)
	if refresh == "" {
		t.Fatalf("login response has no refresh token: %s", raw)
	}

	status, body, raw = request(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{
		"refresh_token": refresh,
	})
	if status != http.StatusOK {
		t.Fatalf("refresh: %d %s", status, raw)
	}
	rotated, _ := dataMap(t, body)["tokens"].(map[string]any)
	newRefresh, _ := rotated["refresh_token"].(string)
	if newRefresh == "" || newRefresh == refresh {
		t.Fatalf("expected a rotated refresh token: %s", raw)
	}

	if status, _, raw := request(t, http.MethodPost, "/api/v1/auth/logout", "", map[string]string{
		"refresh_token": newRefresh,
	}); status != http.StatusOK && status != http.StatusNoContent {
		t.Fatalf("logout: %d %s", status, raw)
	}
	if status, _, _ := request(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{
		"refresh_token": newRefresh,
	}); status == http.StatusOK {
		t.Fatal("revoked refresh token must not be accepted")
	}

	// Profile editing round-trips.
	status, body, raw = request(t, http.MethodPatch, "/api/v1/profiles/me", token, map[string]any{
		"bio": "Integration bio", "city": "Moscow", "interests": []string{"coffee", "cinema"},
	})
	if status != http.StatusOK {
		t.Fatalf("update profile: %d %s", status, raw)
	}
	profile := dataMap(t, body)
	if profile["city"] != "Moscow" || profile["bio"] != "Integration bio" {
		t.Fatalf("profile not updated: %s", raw)
	}

	if status, _, raw := request(t, http.MethodGet, "/api/v1/profiles/"+userID, token, nil); status != http.StatusOK {
		t.Fatalf("get profile by id: %d %s", status, raw)
	}
	if status, _, raw := request(t, http.MethodPost, "/api/v1/profiles/me/location", token, map[string]float64{
		"lat": 91, "lng": 0,
	}); status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid coordinates should be 422, got %d %s", status, raw)
	}
}

func TestSwipeMatchChatAndSafety(t *testing.T) {
	prefix := fmt.Sprintf("flow%d", time.Now().UnixNano())
	tokenA, idA, _ := register(t, prefix+"-a@example.test", "Alice Flow", "female", []string{"male"})
	tokenB, idB, _ := register(t, prefix+"-b@example.test", "Bob Flow", "male", []string{"female"})

	// Self swipe and unknown actions are rejected before touching the database.
	if status, _, _ := request(t, http.MethodPost, "/api/v1/swipes", tokenA, map[string]string{
		"target_user_id": idA, "action": "like",
	}); status != http.StatusUnprocessableEntity && status != http.StatusBadRequest && status != http.StatusForbidden {
		t.Fatalf("self swipe should be rejected, got %d", status)
	}
	if status, _, _ := request(t, http.MethodPost, "/api/v1/swipes", tokenA, map[string]string{
		"target_user_id": idB, "action": "hug",
	}); status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid action should be 422, got %d", status)
	}

	status, body, raw := request(t, http.MethodPost, "/api/v1/swipes", tokenA, map[string]string{
		"target_user_id": idB, "action": "superlike",
	})
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("superlike: %d %s", status, raw)
	}
	if matched, _ := dataMap(t, body)["matched"].(bool); matched {
		t.Fatal("one-sided superlike must not match")
	}

	if status, _, _ := request(t, http.MethodPost, "/api/v1/swipes", tokenA, map[string]string{
		"target_user_id": idB, "action": "like",
	}); status != http.StatusConflict {
		t.Fatalf("duplicate swipe should be 409, got %d", status)
	}

	status, body, raw = request(t, http.MethodPost, "/api/v1/swipes", tokenB, map[string]string{
		"target_user_id": idA, "action": "like",
	})
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("reciprocal like: %d %s", status, raw)
	}
	match := dataMap(t, body)
	if matched, _ := match["matched"].(bool); !matched {
		t.Fatalf("reciprocal like must create a match: %s", raw)
	}
	conversationID, _ := match["conversation_id"].(string)
	if conversationID == "" {
		t.Fatalf("match has no conversation id: %s", raw)
	}

	status, body, raw = request(t, http.MethodGet, "/api/v1/matches", tokenB, nil)
	if status != http.StatusOK {
		t.Fatalf("list matches: %d %s", status, raw)
	}
	if len(dataItems(t, body)) == 0 {
		t.Fatalf("expected at least one match: %s", raw)
	}

	status, body, raw = request(t, http.MethodPost, "/api/v1/conversations/"+conversationID+"/messages", tokenA,
		map[string]string{"body": "hello from the integration suite"})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("send message: %d %s", status, raw)
	}
	messageID, _ := dataMap(t, body)["id"].(string)
	if messageID == "" {
		t.Fatalf("message response has no id: %s", raw)
	}

	status, body, raw = request(t, http.MethodGet, "/api/v1/conversations/"+conversationID+"/messages", tokenB, nil)
	if status != http.StatusOK {
		t.Fatalf("list messages: %d %s", status, raw)
	}
	found := false
	for _, item := range dataItems(t, body) {
		if item["body"] == "hello from the integration suite" {
			found = true
		}
	}
	if !found {
		t.Fatalf("sent message is missing from the conversation: %s", raw)
	}

	if status, _, raw := request(t, http.MethodPost, "/api/v1/conversations/"+conversationID+"/read", tokenB, nil); status != http.StatusOK {
		t.Fatalf("mark read: %d %s", status, raw)
	}
	if status, _, raw := request(t, http.MethodPost, "/api/v1/conversations/"+conversationID+"/typing", tokenA, nil); status != http.StatusOK && status != http.StatusNoContent {
		t.Fatalf("typing: %d %s", status, raw)
	}

	status, body, raw = request(t, http.MethodGet, "/api/v1/notifications/unread-count", tokenB, nil)
	if status != http.StatusOK {
		t.Fatalf("unread count: %d %s", status, raw)
	}
	// Notifications are produced asynchronously from the outbox → Kafka consumer,
	// so allow a short window for the event to be processed.
	deadline := time.Now().Add(15 * time.Second)
	for {
		unread, _ := dataMap(t, body)["unread"].(float64)
		if unread >= 1 {
			break
		}
		if time.Now().After(deadline) {
			status, _, listRaw := request(t, http.MethodGet, "/api/v1/notifications", tokenB, nil)
			t.Fatalf("expected unread notifications for the matched user (status %d): %s", status, listRaw)
		}
		time.Sleep(500 * time.Millisecond)
		status, body, raw = request(t, http.MethodGet, "/api/v1/notifications/unread-count", tokenB, nil)
		if status != http.StatusOK {
			t.Fatalf("unread count: %d %s", status, raw)
		}
	}
	if status, _, raw := request(t, http.MethodPost, "/api/v1/notifications/read-all", tokenB, nil); status != http.StatusOK {
		t.Fatalf("mark all read: %d %s", status, raw)
	}

	// A swipe that produced a match cannot be rewound.
	if status, _, _ := request(t, http.MethodPost, "/api/v1/swipes/rewind", tokenA, nil); status != http.StatusNotFound {
		t.Fatalf("rewind of a matched swipe should be 404, got %d", status)
	}

	// A plain swipe can be rewound, and the target becomes swipeable again.
	_, idC, _ := register(t, prefix+"-c@example.test", "Carol Flow", "female", []string{"male"})
	if status, _, raw := request(t, http.MethodPost, "/api/v1/swipes", tokenA, map[string]string{
		"target_user_id": idC, "action": "dislike",
	}); status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("dislike: %d %s", status, raw)
	}
	status, body, raw = request(t, http.MethodPost, "/api/v1/swipes/rewind", tokenA, nil)
	if status != http.StatusOK {
		t.Fatalf("rewind: %d %s", status, raw)
	}
	rewound, _ := dataMap(t, body)["swipe"].(map[string]any)
	if rewound == nil {
		t.Fatalf("rewind response has no swipe: %s", raw)
	}
	if target, _ := rewound["target_id"].(string); target != idC {
		t.Fatalf("rewind should undo the swipe on %s, got %s", idC, target)
	}
	if status, _, raw := request(t, http.MethodPost, "/api/v1/swipes", tokenA, map[string]string{
		"target_user_id": idC, "action": "like",
	}); status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("swipe after rewind: %d %s", status, raw)
	}

	// Blocking hides the profile from discovery and blocks interaction.
	status, _, raw = request(t, http.MethodPost, "/api/v1/blocks", tokenA, map[string]string{"user_id": idB})
	if status != http.StatusCreated {
		t.Fatalf("block: %d %s", status, raw)
	}
	if status, _, _ := request(t, http.MethodPost, "/api/v1/swipes", tokenB, map[string]string{
		"target_user_id": idA, "action": "like",
	}); status != http.StatusForbidden && status != http.StatusNotFound && status != http.StatusConflict {
		t.Fatalf("blocked interaction should be rejected, got %d", status)
	}
	status, body, raw = request(t, http.MethodGet, "/api/v1/blocks", tokenA, nil)
	if status != http.StatusOK {
		t.Fatalf("list blocks: %d %s", status, raw)
	}
	if count, _ := dataMap(t, body)["count"].(float64); count < 1 {
		t.Fatalf("expected the block to be listed: %s", raw)
	}
	if status, _, raw := request(t, http.MethodDelete, "/api/v1/blocks/"+idB, tokenA, nil); status != http.StatusNoContent {
		t.Fatalf("unblock: %d %s", status, raw)
	}

	// Reporting is accepted and visible to staff.
	status, body, raw = request(t, http.MethodPost, "/api/v1/reports", tokenA, map[string]string{
		"target_user_id": idB, "reason": "spam", "details": "integration report",
	})
	if status != http.StatusCreated {
		t.Fatalf("report: %d %s", status, raw)
	}
	if dataMap(t, body)["id"] == nil {
		t.Fatalf("report response has no id: %s", raw)
	}

	// Swipe limits are always readable and never negative.
	status, body, raw = request(t, http.MethodGet, "/api/v1/swipes/limits", tokenA, nil)
	if status != http.StatusOK {
		t.Fatalf("swipe limits: %d %s", status, raw)
	}
	limits := dataMap(t, body)
	for _, key := range []string{"likes", "superlikes"} {
		entry, _ := limits[key].(map[string]any)
		if entry == nil {
			t.Fatalf("limits.%s missing: %s", key, raw)
		}
		used, _ := entry["used"].(float64)
		limit, _ := entry["limit"].(float64)
		if used > limit {
			t.Fatalf("limits.%s used %v exceeds limit %v", key, used, limit)
		}
	}
}

func TestLegalRequestWorkflow(t *testing.T) {
	adminToken, _ := login(t, "admin@loveservice.app", "AdminDemo123!")
	_, targetID := login(t, "demo@loveservice.app", "LoveDemo123!")

	status, body, raw := request(t, http.MethodPost, "/api/v1/admin/legal-requests", adminToken, map[string]any{
		"case_number":    fmt.Sprintf("CASE-%d", time.Now().UnixNano()),
		"authority":      "Integration Police Department",
		"country":        "RU",
		"legal_basis":    "Court order 42/2026",
		"target_user_id": targetID,
		"scopes":         []string{"account", "profile", "messages"},
		"reason":         "integration lawful-access verification",
	})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("create legal request: %d %s", status, raw)
	}
	requestID, _ := dataMap(t, body)["id"].(string)
	if requestID == "" {
		t.Fatalf("legal request has no id: %s", raw)
	}

	status, body, raw = request(t, http.MethodGet, "/api/v1/admin/legal-requests/"+requestID, adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("get legal request: %d %s", status, raw)
	}
	if statusValue, _ := dataMap(t, body)["status"].(string); statusValue != "pending" {
		t.Fatalf("new legal request should be pending, got %q", statusValue)
	}

	// Export before approval must be refused, whatever the approval policy.
	if status, _, _ := request(t, http.MethodPost, "/api/v1/admin/legal-requests/"+requestID+"/export", adminToken,
		map[string]string{"reason": "premature export"}); status < 400 || status >= 500 {
		t.Fatalf("export of a pending request should be refused with 4xx, got %d", status)
	}

	// With second-admin approval enabled the creator cannot self-approve.
	approveStatus, _, approveRaw := request(t, http.MethodPost, "/api/v1/admin/legal-requests/"+requestID+"/approve", adminToken,
		map[string]string{"reason": "integration approval"})
	switch approveStatus {
	case http.StatusOK:
		status, body, raw = request(t, http.MethodPost, "/api/v1/admin/legal-requests/"+requestID+"/export", adminToken,
			map[string]string{"reason": "integration export"})
		if status != http.StatusOK {
			t.Fatalf("export approved request: %d %s", status, raw)
		}
		export := dataMap(t, body)
		if hash, _ := export["sha256"].(string); len(hash) != 64 {
			t.Fatalf("export must carry a SHA-256 fingerprint: %s", raw)
		}
		if payload, _ := export["data"].(map[string]any); payload == nil {
			t.Fatalf("export has no data payload: %s", raw)
		}
	default:
		if approveStatus < 400 || approveStatus >= 500 {
			t.Fatalf("unexpected approve status %d: %s", approveStatus, approveRaw)
		}
	}

	// Everything is recorded in the audit log.
	status, body, raw = request(t, http.MethodGet, "/api/v1/admin/audit-logs?limit=50", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("audit logs: %d %s", status, raw)
	}
	found := false
	for _, item := range dataItems(t, body) {
		if item["action"] == "legal.request.create" {
			found = true
		}
	}
	if !found {
		t.Fatalf("legal request creation was not audited: %s", raw)
	}
}
