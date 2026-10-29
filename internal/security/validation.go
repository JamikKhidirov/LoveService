package security

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/loveservice/loveservice/internal/domain"
)

var (
	phoneRe       = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)
	displayNameRe = regexp.MustCompile(`^[\p{L}\p{N} _\.\-']{2,40}$`)
)

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func ValidateEmail(email string) error {
	email = NormalizeEmail(email)
	if email == "" {
		return domain.Invalid("email", "email is required")
	}
	if len(email) > 254 {
		return domain.Invalid("email", "email is too long")
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || NormalizeEmail(addr.Address) != email {
		return domain.Invalid("email", "email format is invalid")
	}
	return nil
}

func ValidatePassword(password string) error {
	if len(password) < 8 {
		return domain.Invalid("password", "password must contain at least 8 characters")
	}
	if len(password) > 72 {
		// bcrypt ignores bytes after 72; reject instead of silently truncating.
		return domain.Invalid("password", "password must contain at most 72 characters")
	}
	var upper, lower, digit bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsLower(r):
			lower = true
		case unicode.IsDigit(r):
			digit = true
		}
	}
	if !upper || !lower || !digit {
		return domain.Invalid("password", "password must contain uppercase, lowercase letters and a digit")
	}
	return nil
}

func ValidatePhone(phone string) error {
	if phone == "" {
		return nil
	}
	if !phoneRe.MatchString(phone) {
		return domain.Invalid("phone", "phone must be in E.164 format, e.g. +79991234567")
	}
	return nil
}

func ValidateDisplayName(name string) error {
	name = strings.TrimSpace(name)
	if !displayNameRe.MatchString(name) {
		return domain.Invalid("display_name", "display name must be 2-40 characters and contain letters, digits, spaces, dots, dashes or underscores")
	}
	return nil
}

func ValidateGender(g string) error {
	switch g {
	case domain.GenderMale, domain.GenderFemale, domain.GenderOther:
		return nil
	default:
		return domain.Invalid("gender", "gender must be one of male, female, other")
	}
}

func ValidateBirthDate(t time.Time) error {
	if t.IsZero() {
		return domain.Invalid("birth_date", "birth date is required")
	}
	now := time.Now().UTC()
	if t.After(now) {
		return domain.Invalid("birth_date", "birth date cannot be in the future")
	}
	age := Age(t, now)
	if age < 18 {
		return domain.Invalid("birth_date", "you must be at least 18 years old")
	}
	if age > 120 {
		return domain.Invalid("birth_date", "birth date is invalid")
	}
	return nil
}

func Age(birth time.Time, now time.Time) int {
	if birth.IsZero() {
		return 0
	}
	years := now.Year() - birth.Year()
	if now.Month() < birth.Month() || (now.Month() == birth.Month() && now.Day() < birth.Day()) {
		years--
	}
	return years
}

func ValidateCoordinates(lat, lng float64) error {
	if lat < -90 || lat > 90 {
		return domain.Invalid("lat", "latitude must be between -90 and 90")
	}
	if lng < -180 || lng > 180 {
		return domain.Invalid("lng", "longitude must be between -180 and 180")
	}
	if lat == 0 && lng == 0 {
		return domain.Invalid("lat", "coordinates (0,0) are not a valid location")
	}
	return nil
}

func ValidateInterests(items []string) ([]string, error) {
	if len(items) > 20 {
		return nil, domain.Invalid("interests", "at most 20 interests are allowed")
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if len([]rune(item)) > 30 {
			return nil, domain.Invalid("interests", "each interest must be at most 30 characters")
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out, nil
}

func ValidateBio(bio string) error {
	if len([]rune(bio)) > 1000 {
		return domain.Invalid("bio", "bio must be at most 1000 characters")
	}
	return nil
}

func ValidateMessage(body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return domain.Invalid("body", "message cannot be empty")
	}
	if len([]rune(body)) > 4000 {
		return domain.Invalid("body", "message must be at most 4000 characters")
	}
	return nil
}

func ValidateDateLayout(raw string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, domain.Invalid("birth_date", "birth_date must use YYYY-MM-DD format")
	}
	return t, nil
}

func ValidateLanguage(lang string) error {
	switch lang {
	case "ru", "en", "es", "de", "fr":
		return nil
	default:
		return domain.Invalid("language", "language must be one of ru, en, es, de, fr")
	}
}

func ValidateReportReason(reason string) error {
	switch reason {
	case "spam", "fake_profile", "harassment", "nudity", "underage", "scam", "other":
		return nil
	default:
		return domain.Invalid("reason", "reason must be one of spam, fake_profile, harassment, nudity, underage, scam, other")
	}
}

func ValidatePlatform(platform string) error {
	switch platform {
	case "ios", "android", "web":
		return nil
	default:
		return domain.Invalid("platform", "platform must be one of ios, android, web")
	}
}

func RequireIDs(ids ...string) error {
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("id is required")
		}
	}
	return nil
}
