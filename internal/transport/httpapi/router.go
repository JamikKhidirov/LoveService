// Package httpapi exposes the HTTP transport: router, middleware, handlers and
// request/response mapping. Business rules live in internal/service; this
// package only translates HTTP to service calls.
package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/loveservice/loveservice/internal/cache"
	"github.com/loveservice/loveservice/internal/config"
	"github.com/loveservice/loveservice/internal/observability"
	"github.com/loveservice/loveservice/internal/security"
	"github.com/loveservice/loveservice/internal/service"
	"github.com/loveservice/loveservice/internal/store"
	"github.com/loveservice/loveservice/internal/transport/ws"
)

// Deps is the explicit dependency set of the HTTP layer. It is assembled once
// in internal/app and passed in, so handlers never reach for globals.
type Deps struct {
	Config        *config.Config
	Logger        *slog.Logger
	Store         *store.Store
	Cache         *cache.Cache
	Tokens        *security.TokenManager
	Auth          *service.AuthService
	Profiles      *service.ProfileService
	Discovery     *service.DiscoveryService
	Swipes        *service.SwipeService
	Chat          *service.ChatService
	Notifications *service.NotificationService
	Account       *service.AccountService
	Admin         *service.AdminService
	Premium       *service.PremiumService
	Verification  *service.VerificationService
	WS            *ws.Handler
	MediaHandler  http.Handler
	Version       string
}

// API is the concrete handler set. Each handler method depends only on the
// service it needs, which keeps the transport layer thin.
type API struct {
	cfg           *config.Config
	logger        *slog.Logger
	store         *store.Store
	auth          *service.AuthService
	profiles      *service.ProfileService
	discovery     *service.DiscoveryService
	swipes        *service.SwipeService
	chat          *service.ChatService
	notifications *service.NotificationService
	account       *service.AccountService
	admin         *service.AdminService
	premium       *service.PremiumService
	verification  *service.VerificationService
	ws            *ws.Handler
	version       string
}

func (a *API) log() *slog.Logger {
	if a.logger != nil {
		return a.logger
	}
	return slog.Default()
}

// NewRouter builds the complete HTTP handler tree: middleware -> infrastructure
// routes -> public API -> authenticated API -> role-gated admin API.
func NewRouter(d Deps) http.Handler {
	observability.Init()

	api := &API{
		cfg:           d.Config,
		logger:        d.Logger,
		store:         d.Store,
		auth:          d.Auth,
		profiles:      d.Profiles,
		discovery:     d.Discovery,
		swipes:        d.Swipes,
		chat:          d.Chat,
		notifications: d.Notifications,
		account:       d.Account,
		admin:         d.Admin,
		premium:       d.Premium,
		verification:  d.Verification,
		ws:            d.WS,
		version:       d.Version,
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(observability.Middleware)
	r.Use(requestLogger(api.log()))
	r.Use(securityHeaders)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   d.Config.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		ExposedHeaders:   []string{"X-Request-ID", "X-RateLimit-Limit", "X-RateLimit-Remaining", "Retry-After"},
		AllowCredentials: false,
		MaxAge:           300,
	}))
	r.Use(rateLimit(d.Cache, d.Config.RateLimitPerMinute, api.log()))

	// Infrastructure endpoints (unauthenticated).
	r.Get("/health", api.health)
	r.Get("/ready", api.ready)
	r.Handle("/metrics", promhttp.Handler())
	r.Get("/openapi.yaml", api.openAPI)
	r.Get("/docs", api.swaggerUI)
	r.Get("/", api.swaggerUI)

	if d.MediaHandler != nil {
		r.Handle("/media/*", http.StripPrefix("/media/", d.MediaHandler))
	}

	api.mountRoutes(r, d)
	return r
}
