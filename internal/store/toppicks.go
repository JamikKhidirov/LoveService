package store

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/domain"
)

// ListTopPicks returns today's persisted picks.
func (s *Store) ListTopPicks(ctx context.Context, userID uuid.UUID) ([]domain.TopPick, error) {
	rows, err := s.pool.Query(ctx, `
SELECT `+profileFields+`, tp.rank, tp.score, tp.for_date
FROM top_picks tp
JOIN profiles p ON p.user_id = tp.target_id
JOIN users u ON u.id = p.user_id
WHERE tp.user_id = $1 AND tp.for_date = current_date AND u.status = 'active'
ORDER BY tp.rank ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.TopPick, 0, 5)
	for rows.Next() {
		var item domain.TopPick
		var forDate time.Time
		p, err := scanProfileAnd(rows, &item.Rank, &item.CompatibilityScore, &forDate)
		if err != nil {
			return nil, err
		}
		item.Profile = *p
		item.ForDate = forDate.Format("2006-01-02")
		out = append(out, item)
	}
	return out, rows.Err()
}

// ReplaceTopPicks atomically swaps today's picks for a user.
func (s *Store) ReplaceTopPicks(ctx context.Context, userID uuid.UUID, picks []domain.TopPick) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM top_picks WHERE user_id = $1 AND for_date = current_date`, userID); err != nil {
		return err
	}
	for _, pick := range picks {
		if _, err := tx.Exec(ctx, `
			INSERT INTO top_picks (user_id, target_id, for_date, rank, score)
			VALUES ($1, $2, current_date, $3, $4)
			ON CONFLICT (user_id, for_date, target_id) DO UPDATE
			SET rank = EXCLUDED.rank, score = EXCLUDED.score`,
			userID, pick.Profile.UserID, pick.Rank, pick.CompatibilityScore,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
