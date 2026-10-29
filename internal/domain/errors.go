package domain

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound           = errors.New("resource not found")
	ErrConflict           = errors.New("resource already exists")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrForbidden          = errors.New("forbidden")
	ErrRateLimited        = errors.New("rate limit exceeded")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailTaken         = errors.New("email is already registered")
	ErrInvalidToken       = errors.New("invalid or expired token")
	ErrAlreadySwiped      = errors.New("you have already swiped this profile")
	ErrSelfAction         = errors.New("action is not allowed on your own profile")
	ErrBlocked            = errors.New("interaction is blocked")
	ErrDailyLimitReached  = errors.New("daily limit reached")
	ErrEmailNotVerified   = errors.New("email is not verified")
	ErrMediaLimitReached  = errors.New("media limit reached")
	ErrConversationClosed = errors.New("conversation is not available")
)

// ValidationError carries a field-level validation message.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

func Invalid(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}
