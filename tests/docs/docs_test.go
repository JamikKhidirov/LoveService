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
		"/api/v1/admin/stats:",
		"/api/v1/admin/legal-requests:",
		"/api/v1/admin/legal-requests/{requestID}/export:",
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

func TestPostmanCollection(t *testing.T) {
	var collection struct {
		Info struct {
			Name   string `json:"name"`
			Schema string `json:"schema"`
		} `json:"info"`
		Item []struct {
			Name string `json:"name"`
			Item []struct {
				Name string `json:"name"`
			} `json:"item"`
		} `json:"item"`
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
	for _, want := range []string{"Auth", "Profile", "Media", "Discovery", "Swipes", "Matches & Chat", "Admin (moderator / admin)", "Technical Admin / Lawful Access"} {
		if !folders[want] {
			t.Errorf("postman collection is missing folder %q", want)
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
