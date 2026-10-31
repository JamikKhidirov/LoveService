package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/loveservice/loveservice/internal/domain"
)

// ExtendPremium pushes premium_until forward from the later of now and the
// current expiry, so renewing early never shortens an active subscription.
func (s *Store) ExtendPremium(ctx context.Context, userID uuid.UUID, span time.Duration) (*time.Time, error) {
	var until time.Time
	err := s.pool.QueryRow(ctx, `
		UPDATE users
		SET premium_until = GREATEST(COALESCE(premium_until, now()), now()) + $2::interval,
		    updated_at = now()
		WHERE id = $1
		RETURNING premium_until`, userID, span.String()).Scan(&until)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &until, nil
}

// UnlockIncomingLikes buys a temporary "who liked me" reveal with coins.
func (s *Store) UnlockIncomingLikes(ctx context.Context, userID uuid.UUID, cost int, span time.Duration) (int, *time.Time, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var coins int
	var until time.Time
	err = tx.QueryRow(ctx, `
		UPDATE users
		SET coins = coins - $2,
		    likes_unlocked_until = GREATEST(COALESCE(likes_unlocked_until, now()), now()) + $3::interval,
		    updated_at = now()
		WHERE id = $1 AND coins >= $2
		RETURNING coins, likes_unlocked_until`, userID, cost, span.String()).Scan(&coins, &until)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, userID).Scan(&exists); err != nil {
			return 0, nil, err
		}
		if !exists {
			return 0, nil, domain.ErrNotFound
		}
		return 0, nil, domain.ErrInsufficientCoins
	}
	if err != nil {
		return 0, nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO coin_transactions (user_id, amount, kind, reference, balance_after)
		VALUES ($1, $2, $3, $4, $5)`,
		userID, -cost, domain.CoinKindLikesUnlock, "who_liked_me", coins); err != nil {
		return 0, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, nil, err
	}
	return coins, &until, nil
}

// AddCoins credits a wallet. It is used by the mock top-up endpoint and by
// gift receipts.
func (s *Store) AddCoins(ctx context.Context, userID uuid.UUID, amount int, kind, reference string) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var coins int
	err = tx.QueryRow(ctx, `
		UPDATE users SET coins = coins + $2, updated_at = now()
		WHERE id = $1 RETURNING coins`, userID, amount).Scan(&coins)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO coin_transactions (user_id, amount, kind, reference, balance_after)
		VALUES ($1, $2, $3, $4, $5)`, userID, amount, kind, reference, coins); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return coins, nil
}

// Wallet returns the coin balance plus the most recent movements.
func (s *Store) Wallet(ctx context.Context, userID uuid.UUID, limit int) (*domain.Wallet, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var w domain.Wallet
	err := s.pool.QueryRow(ctx, `
		SELECT coins, premium_until FROM users WHERE id = $1`, userID).Scan(&w.Coins, &w.PremiumUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, amount, kind, reference, balance_after, created_at
		FROM coin_transactions WHERE user_id = $1
		ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	w.Transactions = make([]domain.CoinTransaction, 0, limit)
	for rows.Next() {
		var t domain.CoinTransaction
		if err := rows.Scan(&t.ID, &t.Amount, &t.Kind, &t.Reference, &t.BalanceAfter, &t.CreatedAt); err != nil {
			return nil, err
		}
		w.Transactions = append(w.Transactions, t)
	}
	return &w, rows.Err()
}

// CountIncomingLikes counts visible, not-yet-answered likes.
func (s *Store) CountIncomingLikes(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM swipes s
		JOIN users u ON u.id = s.swiper_id
		WHERE s.target_id = $1 AND s.action IN ('like', 'superlike') AND u.status = 'active'
		  AND NOT EXISTS (SELECT 1 FROM swipes mine WHERE mine.swiper_id = $1 AND mine.target_id = s.swiper_id)`,
		userID).Scan(&n)
	return n, err
}

// SetProfileVerified flips the public verification badge.
func (s *Store) SetProfileVerified(ctx context.Context, userID uuid.UUID, verified bool) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE profiles SET is_verified = $2, updated_at = now() WHERE user_id = $1`, userID, verified)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
