package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/domain"
	"github.com/loveservice/loveservice/internal/store"
)

const (
	// UnlockLikesCostCoins is the one-off price of "who liked me".
	UnlockLikesCostCoins = 100
	// UnlockLikesWindow is how long a one-off unlock stays active.
	UnlockLikesWindow = 24 * time.Hour
	// MaxTopUpCoins bounds the mock top-up endpoint.
	MaxTopUpCoins = 10000
)

type PremiumService struct {
	store *store.Store
}

func NewPremiumService(st *store.Store) *PremiumService {
	return &PremiumService{store: st}
}

func (s *PremiumService) Status(ctx context.Context, userID uuid.UUID) (*domain.PremiumStatus, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	locked, err := s.store.CountIncomingLikes(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &domain.PremiumStatus{
		Active:             user.PremiumActive(),
		PremiumUntil:       user.PremiumUntil,
		Coins:              user.Coins,
		LikesUnlockedUntil: user.LikesUnlockedUntil,
		LockedLikes:        locked,
		UnlockCostCoins:    UnlockLikesCostCoins,
	}, nil
}

// Subscribe is a stand-in for a real payment provider: it extends premium by
// the chosen plan. In production this must only run after a verified payment.
func (s *PremiumService) Subscribe(ctx context.Context, userID uuid.UUID, plan string) (*domain.PremiumStatus, error) {
	var span time.Duration
	switch plan {
	case "", domain.PremiumPlanMonth:
		span = 30 * 24 * time.Hour
	case domain.PremiumPlanYear:
		span = 365 * 24 * time.Hour
	default:
		return nil, domain.Invalid("plan", "plan must be one of month, year")
	}
	if _, err := s.store.ExtendPremium(ctx, userID, span); err != nil {
		return nil, err
	}
	return s.Status(ctx, userID)
}

// UnlockLikes reveals incoming likes for 24 hours. Premium members never pay.
func (s *PremiumService) UnlockLikes(ctx context.Context, userID uuid.UUID) (*domain.PremiumStatus, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user.PremiumActive() || user.LikesUnlocked() {
		return s.Status(ctx, userID)
	}
	if _, _, err := s.store.UnlockIncomingLikes(ctx, userID, UnlockLikesCostCoins, UnlockLikesWindow); err != nil {
		return nil, err
	}
	return s.Status(ctx, userID)
}

func (s *PremiumService) Wallet(ctx context.Context, userID uuid.UUID) (*domain.Wallet, error) {
	return s.store.Wallet(ctx, userID, 20)
}

// TopUp is a mock coin purchase used by the demo. A real implementation must
// verify a store receipt before crediting the wallet.
func (s *PremiumService) TopUp(ctx context.Context, userID uuid.UUID, coins int, reference string) (*domain.Wallet, error) {
	if coins <= 0 || coins > MaxTopUpCoins {
		return nil, domain.Invalid("coins", "coins must be between 1 and 10000")
	}
	if _, err := s.store.AddCoins(ctx, userID, coins, domain.CoinKindTopup, reference); err != nil {
		return nil, err
	}
	return s.Wallet(ctx, userID)
}
