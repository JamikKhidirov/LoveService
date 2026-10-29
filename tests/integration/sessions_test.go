//go:build integration

package integration_test

import (
	"net/http"
	"testing"
)

// TestSessionRevocation covers the account-session surface added in v1.1:
// device listing and "log out everywhere", which must invalidate access tokens
// immediately rather than waiting for their 15 minute TTL.
func TestSessionRevocation(t *testing.T) {
	email := uniqueEmail("sessions")
	_, userID, devCode := register(t, email, "Sessions User", "female", []string{"male"})
	if devCode == "" {
		t.Fatal("expected a development verification code")
	}
	if status, _, raw := request(t, http.MethodPost, "/api/v1/auth/verify-email", "", map[string]string{
		"email": email, "code": devCode,
	}); status != http.StatusOK {
		t.Fatalf("verify email: %d %s", status, raw)
	}
	access, refresh := login(t, email, "StrongPass123!")
	if access == "" || refresh == "" {
		t.Fatal("login did not return a token pair")
	}

	status, body, raw := request(t, http.MethodGet, "/api/v1/devices", access, nil)
	if status != http.StatusOK {
		t.Fatalf("list devices: %d %s", status, raw)
	}
	if count, _ := dataMap(t, body)["count"].(float64); count != 0 {
		t.Fatalf("fresh account should have no devices, got %v: %s", count, raw)
	}

	pushToken := "itest-" + userID
	if status, _, raw := request(t, http.MethodPost, "/api/v1/devices", access, map[string]string{
		"platform": "android", "token": pushToken,
	}); status != http.StatusCreated {
		t.Fatalf("save device: %d %s", status, raw)
	}

	status, body, raw = request(t, http.MethodGet, "/api/v1/devices", access, nil)
	if status != http.StatusOK {
		t.Fatalf("list devices after save: %d %s", status, raw)
	}
	items := dataItems(t, body)
	if len(items) != 1 || items[0]["token"] != pushToken {
		t.Fatalf("saved device not returned: %s", raw)
	}

	if status, _, raw := request(t, http.MethodPost, "/api/v1/auth/logout-all", access, nil); status != http.StatusOK {
		t.Fatalf("logout-all: %d %s", status, raw)
	}
	if status, _, _ := request(t, http.MethodGet, "/api/v1/auth/me", access, nil); status != http.StatusUnauthorized {
		t.Fatalf("access token must be rejected after logout-all, got %d", status)
	}
	if status, _, raw := request(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{
		"refresh_token": refresh,
	}); status != http.StatusUnauthorized {
		t.Fatalf("refresh token must be rejected after logout-all, got %d %s", status, raw)
	}

	// A new login works and the device registration survived.
	access, _ = login(t, email, "StrongPass123!")
	status, body, raw = request(t, http.MethodGet, "/api/v1/devices", access, nil)
	if status != http.StatusOK || len(dataItems(t, body)) != 1 {
		t.Fatalf("devices after re-login: %d %s", status, raw)
	}
}
