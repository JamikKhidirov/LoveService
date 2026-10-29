package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/cache"
	"github.com/loveservice/loveservice/internal/domain"
	"github.com/loveservice/loveservice/internal/security"
)

type ctxKey string

const (
	userIDKey ctxKey = "user_id"
	roleKey   ctxKey = "role"
)

func pathParam(r *http.Request, name string) string { return chi.URLParam(r, name) }

func userIDFrom(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(userIDKey).(uuid.UUID)
	return id
}

func roleFrom(ctx context.Context) string {
	role, _ := ctx.Value(roleKey).(string)
	return role
}

func requireAuth(tokens *security.TokenManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := ""
			auth := r.Header.Get("Authorization")
			if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
				raw = strings.TrimSpace(auth[7:])
			}
			if raw == "" {
				writeError(w, r, slog.Default(), domain.ErrUnauthorized)
				return
			}
			claims, err := tokens.ParseAccess(raw)
			if err != nil {
				writeError(w, r, slog.Default(), domain.ErrInvalidToken)
				return
			}
			id, err := uuid.Parse(claims.UserID)
			if err != nil {
				writeError(w, r, slog.Default(), domain.ErrInvalidToken)
				return
			}
			ctx := context.WithValue(r.Context(), userIDKey, id)
			ctx = context.WithValue(ctx, roleKey, claims.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// requireRole gates moderator / technical-admin endpoints. The JWT role is only
// trusted because it was signed by this service and is re-issued on refresh.
func requireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := allowed[roleFrom(r.Context())]; !ok {
				writeError(w, r, slog.Default(), domain.ErrForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "geolocation=(self), camera=(), microphone=()")
		next.ServeHTTP(w, r)
	})
}

func rateLimit(c *cache.Cache, limit int, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if c == nil || limit <= 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" || r.URL.Path == "/ready" || r.URL.Path == "/metrics" {
				next.ServeHTTP(w, r)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 200*time.Millisecond)
			allowed, remaining, err := c.Allow(ctx, "http:"+clientIP(r), limit, time.Minute)
			cancel()
			if err != nil {
				log.Debug("rate limiter unavailable", "error", err)
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("X-RateLimit-Limit", itoa(limit))
			if remaining >= 0 {
				w.Header().Set("X-RateLimit-Remaining", itoa(remaining))
			}
			if !allowed {
				w.Header().Set("Retry-After", "60")
				writeError(w, r, log, domain.ErrRateLimited)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			log.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", r.Header.Get("X-Request-ID"),
				"remote", clientIP(r),
			)
		})
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
