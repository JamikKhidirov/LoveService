// Package domain_test contains black-box tests for internal/domain.
package domain_test

import (
	"math"
	"testing"

	"github.com/loveservice/loveservice/internal/domain"
)

func TestHaversineKnownDistance(t *testing.T) {
	// Moscow (55.7558, 37.6173) -> Saint Petersburg (59.9343, 30.3351) ~= 634 km.
	got := domain.Haversine(55.7558, 37.6173, 59.9343, 30.3351)
	if math.Abs(got-634) > 15 {
		t.Fatalf("expected ~634 km, got %.2f", got)
	}
}

func TestHaversineZero(t *testing.T) {
	if got := domain.Haversine(10, 20, 10, 20); got != 0 {
		t.Fatalf("expected 0, got %f", got)
	}
}

func TestRadiusTiersGrowFromRequestedRadius(t *testing.T) {
	tiers := domain.RadiusTiers(5)
	if len(tiers) == 0 {
		t.Fatal("expected tiers")
	}
	if tiers[0] != 5 {
		t.Fatalf("first tier must preserve requested radius, got %v", tiers[0])
	}
	for i := 1; i < len(tiers); i++ {
		if tiers[i] <= tiers[i-1] {
			t.Fatalf("tiers must be strictly increasing: %v", tiers)
		}
	}
}

func TestRadiusTiersDedupeAndDefault(t *testing.T) {
	tiers := domain.RadiusTiers(100)
	seen := map[float64]bool{}
	for _, v := range tiers {
		if seen[v] {
			t.Fatalf("duplicate tier %v in %v", v, tiers)
		}
		seen[v] = true
	}
	if got := domain.RadiusTiers(0); got[0] != 10 {
		t.Fatalf("zero radius should default to 10 km, got %v", got)
	}
}

func TestClamp(t *testing.T) {
	cases := []struct{ in, min, max, want float64 }{
		{-5, 0, 10, 0},
		{15, 0, 10, 10},
		{5, 0, 10, 5},
	}
	for _, c := range cases {
		if got := domain.Clamp(c.in, c.min, c.max); got != c.want {
			t.Fatalf("Clamp(%v,%v,%v)=%v want %v", c.in, c.min, c.max, got, c.want)
		}
	}
}
