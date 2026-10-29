// Package security_test contains black-box tests for internal/security.
package security_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/domain"
	"github.com/loveservice/loveservice/internal/security"
)

func TestNormalizeEmail(t *testing.T) {
	if got := security.NormalizeEmail("  User.Name@Example.COM "); got != "user.name@example.com" {
		t.Fatalf("NormalizeEmail = %q", got)
	}
}

func TestValidateEmail(t *testing.T) {
	for _, e := range []string{"a@b.co", "User.Name+tag@example.com"} {
		if err := security.ValidateEmail(e); err != nil {
			t.Fatalf("expected %q valid: %v", e, err)
		}
	}
	for _, e := range []string{"", "not-an-email", "a@", "a b@c.com"} {
		if err := security.ValidateEmail(e); err == nil {
			t.Fatalf("expected %q invalid", e)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	if err := security.ValidatePassword("StrongPass123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, p := range []string{"short1A", "alllowercase1", "ALLUPPERCASE1", "NoDigitsHere"} {
		if err := security.ValidatePassword(p); err == nil {
			t.Fatalf("expected %q invalid", p)
		}
	}
}

func TestValidatePhone(t *testing.T) {
	for _, p := range []string{"+79990001122", "+442071838750"} {
		if err := security.ValidatePhone(p); err != nil {
			t.Fatalf("expected %q valid: %v", p, err)
		}
	}
	for _, p := range []string{"123", "phone", "+7 (999) 000"} {
		if err := security.ValidatePhone(p); err == nil {
			t.Fatalf("expected %q invalid", p)
		}
	}
}

func TestValidateDisplayName(t *testing.T) {
	for _, name := range []string{"Анна", "John Doe", "user_1", "O'Neil"} {
		if err := security.ValidateDisplayName(name); err != nil {
			t.Fatalf("expected %q valid: %v", name, err)
		}
	}
	for _, name := range []string{"", "a", "<script>", "name\nnewline"} {
		if err := security.ValidateDisplayName(name); err == nil {
			t.Fatalf("expected %q invalid", name)
		}
	}
}

func TestValidateGender(t *testing.T) {
	for _, g := range []string{"male", "female", "other"} {
		if err := security.ValidateGender(g); err != nil {
			t.Fatalf("expected %q valid", g)
		}
	}
	if err := security.ValidateGender("unknown"); err == nil {
		t.Fatal("expected invalid gender")
	}
}

func TestValidateBirthDate(t *testing.T) {
	now := time.Now().UTC()
	if err := security.ValidateBirthDate(now.AddDate(-25, 0, 0)); err != nil {
		t.Fatalf("adult should be accepted: %v", err)
	}
	if err := security.ValidateBirthDate(now.AddDate(-10, 0, 0)); err == nil {
		t.Fatal("minor must be rejected")
	}
	if err := security.ValidateBirthDate(now.AddDate(0, 0, 1)); err == nil {
		t.Fatal("future date must be rejected")
	}
}

func TestAge(t *testing.T) {
	birth := time.Date(2000, 6, 15, 0, 0, 0, 0, time.UTC)
	if got := security.Age(birth, time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)); got != 25 {
		t.Fatalf("Age before birthday = %d", got)
	}
	if got := security.Age(birth, time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)); got != 26 {
		t.Fatalf("Age on birthday = %d", got)
	}
}

func TestValidateCoordinates(t *testing.T) {
	if err := security.ValidateCoordinates(55.75, 37.61); err != nil {
		t.Fatal(err)
	}
	if err := security.ValidateCoordinates(91, 0); err == nil {
		t.Fatal("expected latitude error")
	}
	if err := security.ValidateCoordinates(0, 181); err == nil {
		t.Fatal("expected longitude error")
	}
}

func TestContentValidators(t *testing.T) {
	if _, err := security.ValidateInterests([]string{"coffee", "cinema"}); err != nil {
		t.Fatal(err)
	}
	tooMany := make([]string, 21)
	for i := range tooMany {
		tooMany[i] = "interest"
	}
	if _, err := security.ValidateInterests(tooMany); err == nil {
		t.Fatal("expected too many interests to fail")
	}
	if err := security.ValidateBio(strings.Repeat("x", 1001)); err == nil {
		t.Fatal("expected long bio to fail")
	}
	if err := security.ValidateMessage(""); err == nil {
		t.Fatal("expected empty message to fail")
	}
	if err := security.ValidateMessage(strings.Repeat("x", 4001)); err == nil {
		t.Fatal("expected long message to fail")
	}
	if err := security.ValidateLanguage("en"); err != nil {
		t.Fatal(err)
	}
	if err := security.ValidateLanguage("ja"); err == nil {
		t.Fatal("expected unsupported language to fail")
	}
	if err := security.ValidateReportReason("spam"); err != nil {
		t.Fatal(err)
	}
	if err := security.ValidateReportReason("nonsense"); err == nil {
		t.Fatal("expected invalid report reason")
	}
	if err := security.ValidatePlatform("ios"); err != nil {
		t.Fatal(err)
	}
	if err := security.ValidatePlatform("windows"); err == nil {
		t.Fatal("expected invalid platform")
	}
	if err := security.RequireIDs("a", "b"); err != nil {
		t.Fatal(err)
	}
	if err := security.RequireIDs("a", ""); err == nil {
		t.Fatal("expected missing id to fail")
	}
}

func TestValidateDateLayout(t *testing.T) {
	got, err := security.ValidateDateLayout("1998-03-21")
	if err != nil || got.Year() != 1998 {
		t.Fatalf("unexpected result %v %v", got, err)
	}
	if _, err := security.ValidateDateLayout("21.03.1998"); err == nil {
		t.Fatal("expected wrong layout to fail")
	}
}

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := security.HashPassword("StrongPass123!")
	if err != nil {
		t.Fatal(err)
	}
	if !security.CheckPassword(hash, "StrongPass123!") {
		t.Fatal("expected password to match")
	}
	if security.CheckPassword(hash, "wrong") {
		t.Fatal("expected password mismatch")
	}
}

func TestOpaqueTokenHashing(t *testing.T) {
	raw, hash, err := security.NewOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	if raw == "" || hash == "" || raw == hash {
		t.Fatal("expected distinct token and hash")
	}
	if security.HashToken(raw) != hash {
		t.Fatal("hash must be deterministic")
	}
}

func TestAccessTokenRoundTrip(t *testing.T) {
	tm := security.NewTokenManager("01234567890123456789012345678901", 15*time.Minute, 720*time.Hour)
	u := &domain.User{ID: uuid.New(), Email: "a@b.co", Role: domain.RoleAdmin}
	raw, exp, err := tm.IssueAccess(u)
	if err != nil {
		t.Fatal(err)
	}
	if time.Until(exp) <= 0 {
		t.Fatal("expected future expiry")
	}
	claims, err := tm.ParseAccess(raw)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != u.ID.String() || claims.Role != domain.RoleAdmin {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestAccessTokenRejectsForeignSignature(t *testing.T) {
	issuer := security.NewTokenManager("01234567890123456789012345678901", 15*time.Minute, time.Hour)
	verifier := security.NewTokenManager("another-secret-another-secret-32b", 15*time.Minute, time.Hour)
	u := &domain.User{ID: uuid.New(), Email: "a@b.co", Role: domain.RoleUser}
	raw, _, err := issuer.IssueAccess(u)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.ParseAccess(raw); err == nil {
		t.Fatal("expected foreign token to be rejected")
	}
}

func TestAccessTokenExpires(t *testing.T) {
	tm := security.NewTokenManager("01234567890123456789012345678901", -time.Minute, time.Hour)
	u := &domain.User{ID: uuid.New(), Email: "a@b.co", Role: domain.RoleUser}
	raw, _, err := tm.IssueAccess(u)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tm.ParseAccess(raw); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}
