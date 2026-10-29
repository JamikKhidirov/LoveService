package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/loveservice/loveservice/internal/domain"
)

func (s *Store) ListMatches(ctx context.Context, userID uuid.UUID) ([]domain.Match, error) {
	rows, err := s.pool.Query(ctx, `
SELECT `+profileFields+`, m.id, m.user_a, m.user_b, m.created_at, c.id
FROM matches m
JOIN conversations c ON c.match_id = m.id
JOIN profiles p ON p.user_id = CASE WHEN m.user_a = $1 THEN m.user_b ELSE m.user_a END
JOIN users u ON u.id = p.user_id
WHERE (m.user_a = $1 OR m.user_b = $1)
  AND m.unmatched_at IS NULL
  AND u.status = 'active'
ORDER BY COALESCE(c.last_message_at, m.created_at) DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Match, 0, 20)
	for rows.Next() {
		var m domain.Match
		var convID uuid.UUID
		p, err := scanProfileAnd(rows, &m.ID, &m.UserA, &m.UserB, &m.CreatedAt, &convID)
		if err != nil {
			return nil, err
		}
		m.Peer = p
		m.ConversationID = &convID
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) GetMatch(ctx context.Context, userID, matchID uuid.UUID) (*domain.Match, error) {
	var m domain.Match
	var convID uuid.UUID
	row := s.pool.QueryRow(ctx, `
SELECT `+profileFields+`, m.id, m.user_a, m.user_b, m.created_at, c.id
FROM matches m
JOIN conversations c ON c.match_id = m.id
JOIN profiles p ON p.user_id = CASE WHEN m.user_a = $1 THEN m.user_b ELSE m.user_a END
WHERE m.id = $2
  AND (m.user_a = $1 OR m.user_b = $1)
  AND m.unmatched_at IS NULL`, userID, matchID)
	p, err := scanProfileAnd(row, &m.ID, &m.UserA, &m.UserB, &m.CreatedAt, &convID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.Peer = p
	m.ConversationID = &convID
	return &m, nil
}

func (s *Store) Unmatch(ctx context.Context, userID, matchID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userA, userB uuid.UUID
	err = tx.QueryRow(ctx, `
		UPDATE matches SET unmatched_at = now()
		WHERE id = $1 AND (user_a = $2 OR user_b = $2) AND unmatched_at IS NULL
		RETURNING user_a, user_b`, matchID, userID,
	).Scan(&userA, &userB)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := enqueueTx(ctx, tx, "love.events", "match.unmatched", matchID.String(), map[string]any{
		"match_id": matchID,
		"user_a":   userA,
		"user_b":   userB,
		"by_user":  userID,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) AreMatched(ctx context.Context, userA, userB uuid.UUID) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM matches
			WHERE unmatched_at IS NULL
			  AND ((user_a = $1 AND user_b = $2) OR (user_a = $2 AND user_b = $1))
		)`, userA, userB).Scan(&ok)
	return ok, err
}
