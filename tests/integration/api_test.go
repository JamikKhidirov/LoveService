//go:build integration

// Package integration contains end-to-end tests against a running stack.
// Start it with: docker compose up -d --build
// Then run: go test -tags=integration ./tests/integration/...
package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func baseURL() string {
	if v := os.Getenv("LOVESERVICE_BASE_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://localhost:8080"
}

func request(t *testing.T, method, path, token string, body any) (int, map[string]any, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, baseURL()+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var envelope map[string]any
	_ = json.Unmarshal(raw, &envelope)
	return resp.StatusCode, envelope, raw
}

func dataMap(t *testing.T, envelope map[string]any) map[string]any {
	t.Helper()
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("response has no data object: %v", envelope)
	}
	return data
}

func login(t *testing.T, email, password string) (string, string) {
	t.Helper()
	status, body, raw := request(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"email": email, "password": password,
	})
	if status != http.StatusOK {
		t.Fatalf("login %s: status %d body %s", email, status, raw)
	}
	data := dataMap(t, body)
	tokens, ok := data["tokens"].(map[string]any)
	if !ok {
		t.Fatalf("login response has no tokens: %s", raw)
	}
	user, _ := data["user"].(map[string]any)
	access, _ := tokens["access_token"].(string)
	id, _ := user["id"].(string)
	if access == "" || id == "" {
		t.Fatalf("login response incomplete: %s", raw)
	}
	return access, id
}

func TestHealthAndDocs(t *testing.T) {
	for _, path := range []string{"/health", "/ready", "/docs", "/openapi.yaml"} {
		status, _, raw := request(t, http.MethodGet, path, "", nil)
		if status != http.StatusOK {
			t.Fatalf("%s returned %d: %s", path, status, raw)
		}
	}
}

func TestDemoUserFlow(t *testing.T) {
	token, userID := login(t, "demo@loveservice.app", "LoveDemo123!")
	_ = userID

	status, body, raw := request(t, http.MethodGet, "/api/v1/auth/me", token, nil)
	if status != http.StatusOK {
		t.Fatalf("auth/me: %d %s", status, raw)
	}
	if dataMap(t, body)["profile"] == nil {
		t.Fatalf("auth/me has no profile: %s", raw)
	}

	if status, _, raw := request(t, http.MethodPost, "/api/v1/profiles/me/location", token, map[string]float64{
		"lat": 55.7558, "lng": 37.6173,
	}); status != http.StatusOK {
		t.Fatalf("update location: %d %s", status, raw)
	}

	status, body, raw = request(t, http.MethodGet, "/api/v1/discovery?lat=55.7558&lng=37.6173&radius_km=5&expand=true&limit=10", token, nil)
	if status != http.StatusOK {
		t.Fatalf("discovery: %d %s", status, raw)
	}
	data := dataMap(t, body)
	if _, ok := data["profiles"]; !ok {
		t.Fatalf("discovery has no profiles field: %s", raw)
	}

	for _, path := range []string{
		"/api/v1/discovery/stats",
		"/api/v1/discovery/likes",
		"/api/v1/swipes/limits",
		"/api/v1/matches",
		"/api/v1/conversations",
		"/api/v1/notifications",
		"/api/v1/notifications/unread-count",
		"/api/v1/blocks",
		"/api/v1/profiles/me/preferences",
		"/api/v1/profiles/me/settings",
		"/api/v1/account/export",
	} {
		if status, _, raw := request(t, http.MethodGet, path, token, nil); status != http.StatusOK {
			t.Fatalf("%s: %d %s", path, status, raw)
		}
	}
}

func TestMediaUploadAndDelete(t *testing.T) {
	token, _ := login(t, "demo@loveservice.app", "LoveDemo123!")

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "smoke.png")
	if err != nil {
		t.Fatal(err)
	}
	// 1x1 transparent PNG.
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
		0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
		0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
	if _, err := part.Write(png); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL()+"/api/v1/profiles/me/photos", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload: %d %s", resp.StatusCode, raw)
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	media := dataMap(t, envelope)
	mediaID, _ := media["id"].(string)
	mediaURL, _ := media["url"].(string)
	if mediaID == "" || mediaURL == "" {
		t.Fatalf("upload response incomplete: %s", raw)
	}

	public, err := http.Get(mediaURL)
	if err != nil {
		t.Fatalf("fetch uploaded media: %v", err)
	}
	public.Body.Close()
	if public.StatusCode != http.StatusOK {
		t.Fatalf("uploaded media is not publicly readable: %d", public.StatusCode)
	}

	if status, _, raw := request(t, http.MethodDelete, "/api/v1/profiles/me/photos/"+mediaID, token, nil); status != http.StatusNoContent {
		t.Fatalf("delete media: %d %s", status, raw)
	}
}

func TestAdminAndLawfulAccessSurface(t *testing.T) {
	token, adminID := login(t, "admin@loveservice.app", "AdminDemo123!")
	_ = adminID

	for _, path := range []string{
		"/api/v1/admin/stats",
		"/api/v1/admin/users?limit=5",
		"/api/v1/admin/reports",
		"/api/v1/admin/audit-logs?limit=5",
		"/api/v1/admin/legal-requests",
	} {
		if status, _, raw := request(t, http.MethodGet, path, token, nil); status != http.StatusOK {
			t.Fatalf("%s: %d %s", path, status, raw)
		}
	}

	_, targetID := login(t, "demo@loveservice.app", "LoveDemo123!")
	status, _, raw := request(t, http.MethodGet, "/api/v1/admin/users/"+targetID+"?reason=integration+audit", token, nil)
	if status != http.StatusOK {
		t.Fatalf("admin user detail: %d %s", status, raw)
	}
}

func TestModeratorCannotUseAdminOnlyRoutes(t *testing.T) {
	token, _ := login(t, "moderator@loveservice.app", "ModeratorDemo123!")
	status, _, raw := request(t, http.MethodGet, "/api/v1/admin/audit-logs", token, nil)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", status, raw)
	}
}

func TestWebSocketPingPong(t *testing.T) {
	token, _ := login(t, "demo@loveservice.app", "LoveDemo123!")
	u, err := url.Parse(baseURL())
	if err != nil {
		t.Fatal(err)
	}
	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	wsURL := fmt.Sprintf("%s://%s/api/v1/ws?token=%s", scheme, u.Host, url.QueryEscape(token))

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	var ready map[string]any
	if err := conn.ReadJSON(&ready); err != nil {
		t.Fatalf("read ready event: %v", err)
	}
	if ready["type"] != "ready" {
		t.Fatalf("expected ready event, got %v", ready)
	}

	if err := conn.WriteJSON(map[string]any{"type": "ping"}); err != nil {
		t.Fatal(err)
	}
	var pong map[string]any
	if err := conn.ReadJSON(&pong); err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if pong["type"] != "pong" {
		t.Fatalf("expected pong, got %v", pong)
	}
}

func TestUnauthenticatedIsRejected(t *testing.T) {
	status, _, _ := request(t, http.MethodGet, "/api/v1/profiles/me", "", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}
