// Package httpapi_test contains black-box tests for internal/transport/httpapi.
// The router is exercised through net/http/httptest without a database.
package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/loveservice/loveservice/internal/config"
	"github.com/loveservice/loveservice/internal/transport/httpapi"
)

func newRouter(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	spec := "openapi: 3.1.0\ninfo:\n  title: test\n  version: 1.0.0\npaths: {}\n"
	if err := os.WriteFile(filepath.Join(dir, "openapi.yaml"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Env:                "development",
		DocsDir:            dir,
		CORSOrigins:        []string{"*"},
		RateLimitPerMinute: 0,
	}
	return httpapi.NewRouter(httpapi.Deps{Config: cfg, Version: "test-version"})
}

func TestHealth(t *testing.T) {
	rr := httptest.NewRecorder()
	newRouter(t).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var body struct {
		Data struct {
			Status  string `json:"status"`
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Status != "ok" || body.Data.Version != "test-version" {
		t.Fatalf("body = %s", rr.Body.String())
	}
}

func TestReadyWithoutDatabase(t *testing.T) {
	rr := httptest.NewRecorder()
	newRouter(t).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
}

func TestSwaggerUI(t *testing.T) {
	for _, path := range []string{"/docs", "/"} {
		rr := httptest.NewRecorder()
		newRouter(t).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("%s status = %d", path, rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "SwaggerUIBundle") {
			t.Fatalf("%s does not render Swagger UI", path)
		}
	}
}

func TestOpenAPISpecIsServed(t *testing.T) {
	rr := httptest.NewRecorder()
	newRouter(t).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "openapi: 3.1.0") {
		t.Fatalf("body = %s", rr.Body.String())
	}
}

func TestProtectedRouteRequiresToken(t *testing.T) {
	for _, path := range []string{"/api/v1/profiles/me", "/api/v1/admin/stats"} {
		rr := httptest.NewRecorder()
		newRouter(t).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, body = %s", path, rr.Code, rr.Body.String())
		}
	}
}

func TestUnknownRoute(t *testing.T) {
	rr := httptest.NewRecorder()
	newRouter(t).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rr.Code)
	}
}
