package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/loveservice/loveservice/internal/domain"
)

func TestProfileBirthDateString(t *testing.T) {
	p := domain.Profile{BirthDate: time.Date(1998, 3, 21, 0, 0, 0, 0, time.UTC)}
	if got := p.BirthDateString(); got != "1998-03-21" {
		t.Fatalf("BirthDateString = %q", got)
	}
}

func TestValidationError(t *testing.T) {
	err := domain.Invalid("email", "invalid email")
	var v *domain.ValidationError
	if !errors.As(err, &v) {
		t.Fatal("expected *ValidationError")
	}
	if v.Field != "email" || v.Message != "invalid email" {
		t.Fatalf("unexpected validation error %+v", v)
	}
	if err.Error() != "email: invalid email" {
		t.Fatalf("Error() = %q", err.Error())
	}
	if (&domain.ValidationError{Message: "only message"}).Error() != "only message" {
		t.Fatal("field-less validation error should return the message")
	}
}

func TestSentinelErrorsAreDistinct(t *testing.T) {
	sentinels := []error{
		domain.ErrNotFound, domain.ErrConflict, domain.ErrUnauthorized,
		domain.ErrForbidden, domain.ErrRateLimited, domain.ErrInvalidCredentials,
		domain.ErrEmailTaken, domain.ErrInvalidToken, domain.ErrAlreadySwiped,
		domain.ErrSelfAction, domain.ErrBlocked, domain.ErrDailyLimitReached,
		domain.ErrEmailNotVerified, domain.ErrMediaLimitReached, domain.ErrConversationClosed,
	}
	for i, a := range sentinels {
		for j, b := range sentinels {
			if i != j && errors.Is(a, b) {
				t.Fatalf("sentinel %d matches %d", i, j)
			}
		}
	}
}
