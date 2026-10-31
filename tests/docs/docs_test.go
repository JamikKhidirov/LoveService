// Package docs_test validates the published OpenAPI and Postman artefacts.
package docs_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func readDoc(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile("../../docs/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}

func TestOpenAPIDocument(t *testing.T) {
	spec := readDoc(t, "openapi.yaml")

	for _, want := range []string{
		"openapi: 3.1.0",
		"title: LoveService API",
		"bearerAuth:",
		"/api/v1/auth/register:",
		"/api/v1/auth/login:",
		"/api/v1/discovery:",
		"/api/v1/swipes:",
		"/api/v1/matches:",
		"/api/v1/conversations:",
		"/api/v1/account/export:",
		"/api/v1/sessions:",
		"/api/v1/sessions/{sessionID}:",
		"/api/v1/profile/stats:",
		"/api/v1/recommendations/top-picks:",
		"/api/v1/discovery/likes/unlock:",
		"/api/v1/premium:",
		"/api/v1/premium/subscribe:",
		"/api/v1/wallet:",
		"/api/v1/wallet/topup:",
		"/api/v1/gifts/catalog:",
		"/api/v1/verification/face:",
		"/api/v1/chat/{conversationID}/gifts:",
		"/api/v1/chat/{conversationID}/media:",
		"/api/v1/admin/stats:",
		"/api/v1/admin/legal-requests:",
		"/api/v1/admin/legal-requests/{requestID}/export:",
		"Premium",
		"Verification",
		"TechnicalAdmin",
		"Lawful access only",
	} {
		if !strings.Contains(spec, want) {
			t.Errorf("openapi.yaml is missing %q", want)
		}
	}
	if strings.Contains(spec, "openapi: 3.0") {
		t.Error("unexpected OpenAPI 3.0 marker")
	}
}

type postmanItem struct {
	Name string        `json:"name"`
	Item []postmanItem `json:"item"`
}

func TestPostmanCollection(t *testing.T) {
	var collection struct {
		Info struct {
			Name   string `json:"name"`
			Schema string `json:"schema"`
		} `json:"info"`
		Item []postmanItem `json:"item"`
	}
	if err := json.Unmarshal([]byte(readDoc(t, "postman_collection.json")), &collection); err != nil {
		t.Fatalf("invalid postman JSON: %v", err)
	}
	if collection.Info.Name != "LoveService API" {
		t.Fatalf("collection name = %q", collection.Info.Name)
	}
	if !strings.Contains(collection.Info.Schema, "v2.1.0") {
		t.Fatalf("unexpected schema %q", collection.Info.Schema)
	}

	folders := map[string]bool{}
	for _, item := range collection.Item {
		folders[item.Name] = true
	}
	for _, want := range []string{"Auth", "Profile", "Media", "Discovery", "Swipes", "Matches & Chat", "Premium & Gifts", "Safety & Devices", "Admin (moderator / admin)", "Technical Admin / Lawful Access"} {
		if !folders[want] {
			t.Errorf("postman collection is missing folder %q", want)
		}
	}

	// New v1.2 requests must be importable and visible in the collection.
	names := map[string]bool{}
	var walk func(items []postmanItem)
	walk = func(items []postmanItem) {
		for _, item := range items {
			names[item.Name] = true
			walk(item.Item)
		}
	}
	walk(collection.Item)
	for _, want := range []string{
		"List sessions", "Revoke session", "Profile stats",
		"Face verification status", "Submit face selfie", "Top picks",
		"Unlock who liked me", "Premium status", "Subscribe (month)",
		"Wallet", "Top up 500 coins", "Gift catalog", "Send gift",
		"Send chat media",
	} {
		if !names[want] {
			t.Errorf("postman collection is missing request %q", want)
		}
	}
}

func TestFeatureDocumentation(t *testing.T) {
	doc := readDoc(t, "FEATURES.md")
	for _, want := range []string{
		"Who liked me", "Premium", "elo_rating", "Daily top picks",
		"Profile statistics", "Device and session management",
		"Face verification", "FACE_VERIFY_URL", "Gift", "rose",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("FEATURES.md is missing %q", want)
		}
	}
	api := readDoc(t, "API.md")
	for _, want := range []string{
		"/api/v1/sessions", "/api/v1/profile/stats",
		"/api/v1/recommendations/top-picks", "/api/v1/premium",
		"/api/v1/verification/face", "premium_required", "insufficient_coins",
	} {
		if !strings.Contains(api, want) {
			t.Errorf("API.md is missing %q", want)
		}
	}
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	for _, want := range []string{"Device management", "/api/v1/sessions", "X-Device-Model", "Premium", "face"} {
		if !strings.Contains(string(readme), want) {
			t.Errorf("README.md is missing %q", want)
		}
	}
}

func TestEmailDocumentation(t *testing.T) {
	doc := readDoc(t, "EMAIL.md")
	for _, want := range []string{
		"SMTP_HOST", "SMTP_TLS", "starttls", "MAIL_OUTBOX_DIR",
		"smtp.gmail.com", "smtp.yandex.ru", "multipart/alternative",
		"email-preview.html", "SPF", "DKIM", "DMARC",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("EMAIL.md is missing %q", want)
		}
	}
	preview := readDoc(t, "email-preview.html")
	if !strings.Contains(preview, "<!DOCTYPE html>") || !strings.Contains(preview, "LoveService") {
		t.Error("email-preview.html does not look like a rendered preview")
	}
}

func TestDockerComposeHasFullStack(t *testing.T) {
	body, err := os.ReadFile("../../docker-compose.yml")
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	compose := string(body)
	for _, service := range []string{"postgres:", "redis:", "kafka:", "kafka-ui:", "minio:", "api:"} {
		if !strings.Contains(compose, service) {
			t.Errorf("docker-compose.yml is missing service %q", service)
		}
	}
}
