package domain_test

import (
	"math"
	"testing"

	"github.com/loveservice/loveservice/internal/domain"
)

func TestGiftCatalogIsConsistent(t *testing.T) {
	if len(domain.GiftCatalog) < 4 {
		t.Fatalf("expected a small but useful catalogue, got %d gifts", len(domain.GiftCatalog))
	}
	seen := map[string]struct{}{}
	lastPrice := 0
	for _, gift := range domain.GiftCatalog {
		if gift.Code == "" || gift.Name == "" || gift.Emoji == "" {
			t.Fatalf("gift %+v has empty fields", gift)
		}
		if gift.Coins <= 0 {
			t.Fatalf("gift %s must cost more than zero coins", gift.Code)
		}
		if gift.Coins <= lastPrice {
			t.Fatalf("gift catalogue must be ordered by ascending price: %s", gift.Code)
		}
		lastPrice = gift.Coins
		if _, ok := seen[gift.Code]; ok {
			t.Fatalf("duplicate gift code %q", gift.Code)
		}
		seen[gift.Code] = struct{}{}
	}
}

func TestFindGiftIsCaseInsensitive(t *testing.T) {
	gift, ok := domain.FindGift("  ROSE ")
	if !ok {
		t.Fatal("expected rose to be found")
	}
	if gift.Code != "rose" || gift.Coins != 10 {
		t.Fatalf("unexpected gift: %+v", gift)
	}
	if _, ok := domain.FindGift("unicorn"); ok {
		t.Fatal("unknown gift must not be found")
	}
}

func TestExpectedScoreIsSymmetric(t *testing.T) {
	if got := domain.ExpectedScore(1000, 1000); math.Abs(got-0.5) > 1e-9 {
		t.Fatalf("equal ratings must give 0.5, got %v", got)
	}
	low := domain.ExpectedScore(1000, 1400)
	high := domain.ExpectedScore(1400, 1000)
	if low >= 0.5 || high <= 0.5 {
		t.Fatalf("expected underdog < 0.5 and favourite > 0.5, got %v/%v", low, high)
	}
	if math.Abs(low+high-1) > 1e-9 {
		t.Fatalf("expected probabilities to be complementary, got %v + %v", low, high)
	}
}

func TestNextELORatingMovesTowardsOutcomeAndClamps(t *testing.T) {
	liked := domain.NextELORating(1000, 1000, 1, 16)
	if liked <= 1000 {
		t.Fatalf("a like must increase the rating, got %v", liked)
	}
	disliked := domain.NextELORating(1000, 1000, 0, 8)
	if disliked >= 1000 {
		t.Fatalf("a dislike must decrease the rating, got %v", disliked)
	}
	if got := domain.NextELORating(domain.MaxELORating, 100, 1, 100); got != domain.MaxELORating {
		t.Fatalf("rating must clamp at the maximum, got %v", got)
	}
	if got := domain.NextELORating(domain.MinELORating, 3000, 0, 100); got != domain.MinELORating {
		t.Fatalf("rating must clamp at the minimum, got %v", got)
	}
}

func TestELOParamsForAction(t *testing.T) {
	for _, action := range []string{domain.ActionLike, domain.ActionSuperlike, domain.ActionDislike} {
		outcome, k, ok := domain.ELOParamsForAction(action)
		if !ok || k <= 0 {
			t.Fatalf("action %q should have ELO parameters", action)
		}
		if outcome != 0 && outcome != 1 {
			t.Fatalf("action %q has unexpected outcome %v", action, outcome)
		}
	}
	if _, _, ok := domain.ELOParamsForAction("hug"); ok {
		t.Fatal("unknown actions must not affect ELO")
	}
}

func TestRatingLevelIsNormalised(t *testing.T) {
	if got := domain.RatingLevel(domain.MinELORating); got != 0 {
		t.Fatalf("minimum rating must map to 0, got %v", got)
	}
	if got := domain.RatingLevel(domain.MaxELORating); got != 1 {
		t.Fatalf("maximum rating must map to 1, got %v", got)
	}
	if got := domain.RatingLevel(-500); got != 0 {
		t.Fatalf("below-minimum rating must clamp to 0, got %v", got)
	}
	if got := domain.RatingLevel(99999); got != 1 {
		t.Fatalf("above-maximum rating must clamp to 1, got %v", got)
	}
}
