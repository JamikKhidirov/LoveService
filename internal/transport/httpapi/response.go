package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/domain"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": payload})
}

func writeError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	status, code, message, details := mapError(err)
	if status >= 500 {
		log.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{Error: errorDetail{Code: code, Message: message, Details: details}})
}

// wrappedMessage keeps the explanatory part of an error that wraps a domain
// sentinel (for example "forbidden: second-admin approval required") so clients
// receive an actionable message instead of the generic fallback.
func wrappedMessage(err, sentinel error, fallback string) string {
	prefix := sentinel.Error() + ": "
	if msg := strings.TrimPrefix(err.Error(), prefix); msg != err.Error() && msg != "" {
		return msg
	}
	return fallback
}

func mapError(err error) (int, string, string, map[string]any) {
	var v *domain.ValidationError
	switch {
	case errors.As(err, &v):
		return http.StatusUnprocessableEntity, "validation_error", v.Message, map[string]any{"field": v.Field}
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, "not_found", "resource not found", nil
	case errors.Is(err, domain.ErrEmailTaken):
		return http.StatusConflict, "email_taken", "email is already registered", nil
	case errors.Is(err, domain.ErrAlreadySwiped):
		return http.StatusConflict, "already_swiped", "you have already swiped this profile", nil
	case errors.Is(err, domain.ErrConflict):
		return http.StatusConflict, "conflict", wrappedMessage(err, domain.ErrConflict, "resource already exists"), nil
	case errors.Is(err, domain.ErrInvalidCredentials):
		return http.StatusUnauthorized, "invalid_credentials", "invalid email or password", nil
	case errors.Is(err, domain.ErrInvalidToken):
		return http.StatusUnauthorized, "invalid_token", "token is invalid or expired", nil
	case errors.Is(err, domain.ErrUnauthorized):
		return http.StatusUnauthorized, "unauthorized", "authentication is required", nil
	case errors.Is(err, domain.ErrEmailNotVerified):
		return http.StatusForbidden, "email_not_verified", "please verify your email first", nil
	case errors.Is(err, domain.ErrBlocked):
		return http.StatusForbidden, "blocked", "interaction is blocked", nil
	case errors.Is(err, domain.ErrForbidden):
		return http.StatusForbidden, "forbidden", wrappedMessage(err, domain.ErrForbidden, "you are not allowed to perform this action"), nil
	case errors.Is(err, domain.ErrAccountInactive):
		return http.StatusForbidden, "account_inactive", "this account is not active", nil
	case errors.Is(err, domain.ErrConversationClosed):
		return http.StatusForbidden, "conversation_closed", "conversation is not available", nil
	case errors.Is(err, domain.ErrPremiumRequired):
		return http.StatusPaymentRequired, "premium_required", "premium subscription required", nil
	case errors.Is(err, domain.ErrInsufficientCoins):
		return http.StatusPaymentRequired, "insufficient_coins", "not enough coins for this action", nil
	case errors.Is(err, domain.ErrPhotoRequired):
		return http.StatusUnprocessableEntity, "photo_required", "add a profile photo before verifying", nil
	case errors.Is(err, domain.ErrVerificationFailed):
		return http.StatusBadGateway, "verification_failed", wrappedMessage(err, domain.ErrVerificationFailed, "face verification failed"), nil
	case errors.Is(err, domain.ErrSelfAction):
		return http.StatusUnprocessableEntity, "self_action", "action is not allowed on your own profile", nil
	case errors.Is(err, domain.ErrDailyLimitReached):
		return http.StatusTooManyRequests, "daily_limit_reached", "daily limit reached, try again tomorrow", nil
	case errors.Is(err, domain.ErrRateLimited):
		return http.StatusTooManyRequests, "rate_limited", "too many requests", nil
	case errors.Is(err, domain.ErrMediaLimitReached):
		return http.StatusUnprocessableEntity, "media_limit_reached", "media limit reached", nil
	default:
		return http.StatusInternalServerError, "internal_error", "internal server error", nil
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return domain.Invalid("body", "request body must be valid JSON")
	}
	return nil
}

func parseUUIDParam(r *http.Request, name string) (uuid.UUID, error) {
	raw := strings.TrimSpace(pathParam(r, name))
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, domain.Invalid(name, name+" must be a valid UUID")
	}
	return id, nil
}

func parseTimeQuery(r *http.Request, name string) (*time.Time, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05", raw)
	}
	if err != nil {
		return nil, domain.Invalid(name, name+" must be an RFC3339 timestamp")
	}
	return &t, nil
}

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return strings.TrimSpace(strings.Split(ip, ",")[0])
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.TrimSpace(strings.Split(ip, ",")[0])
	}
	return r.RemoteAddr
}
