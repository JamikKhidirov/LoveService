// Package observability_test contains black-box tests for internal/observability.
package observability_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/loveservice/loveservice/internal/config"
	"github.com/loveservice/loveservice/internal/observability"
)

func TestNewLogger(t *testing.T) {
	for _, env := range []string{"development", "production"} {
		log := observability.NewLogger(&config.Config{Env: env, LogLevel: "debug"})
		if log == nil {
			t.Fatalf("%s logger is nil", env)
		}
		if log.Enabled(nil, slog.LevelDebug) == false {
			t.Fatalf("%s logger should enable debug", env)
		}
	}
}

func TestMiddlewareRecordsRequest(t *testing.T) {
	observability.Init()
	observability.Init() // idempotent, must not panic on duplicate registration

	r := chi.NewRouter()
	r.Use(observability.Middleware)
	r.Get("/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rr.Code)
	}
}
