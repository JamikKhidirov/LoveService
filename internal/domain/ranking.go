package domain

import (
	"math"
	"strings"
)

// GiftCatalog is the fixed catalogue of virtual gifts. Prices are expressed in
// coins; the sender is debited and the recipient credited atomically.
var GiftCatalog = []Gift{
	{Code: "rose", Name: "Rose", Emoji: "🌹", Coins: 10},
	{Code: "heart", Name: "Heart", Emoji: "❤️", Coins: 25},
	{Code: "teddy", Name: "Teddy Bear", Emoji: "🧸", Coins: 50},
	{Code: "ring", Name: "Ring", Emoji: "💍", Coins: 200},
	{Code: "crown", Name: "Crown", Emoji: "👑", Coins: 500},
}

// FindGift resolves a gift code case-insensitively.
func FindGift(code string) (Gift, bool) {
	code = strings.ToLower(strings.TrimSpace(code))
	for _, g := range GiftCatalog {
		if g.Code == code {
			return g, true
		}
	}
	return Gift{}, false
}

const (
	// DefaultELORating is the starting attractiveness rating.
	DefaultELORating = 1000.0
	// MinELORating and MaxELORating clamp runaway values.
	MinELORating = 100.0
	MaxELORating = 3000.0
)

// ExpectedScore returns the logistic expectation that the target profile is
// liked by someone with the given opponent rating.
func ExpectedScore(rating, opponent float64) float64 {
	return 1 / (1 + math.Pow(10, (opponent-rating)/400))
}

// NextELORating applies one observed outcome (1 = like, 0 = dislike) to the
// target's rating. The rating moves towards the outcome, so profiles that are
// liked by many people climb the discovery ordering.
func NextELORating(rating, opponent, outcome, k float64) float64 {
	next := rating + k*(outcome-ExpectedScore(rating, opponent))
	if next < MinELORating {
		return MinELORating
	}
	if next > MaxELORating {
		return MaxELORating
	}
	return next
}

// ELOParamsForAction maps a swipe action to the ELO outcome and K-factor.
// A like is a strong positive signal, a superlike an even stronger one and a
// dislike only a mild negative one (passing is not the same as rejecting).
func ELOParamsForAction(action string) (outcome, k float64, ok bool) {
	switch action {
	case ActionLike:
		return 1, 16, true
	case ActionSuperlike:
		return 1, 24, true
	case ActionDislike:
		return 0, 8, true
	default:
		return 0, 0, false
	}
}

// RatingLevel converts a raw ELO rating into a 0..1 signal for ordering.
func RatingLevel(rating float64) float64 {
	level := (rating - MinELORating) / (MaxELORating - MinELORating)
	if level < 0 {
		return 0
	}
	if level > 1 {
		return 1
	}
	return level
}
