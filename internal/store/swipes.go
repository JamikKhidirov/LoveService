package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/loveservice/loveservice/internal/domain"
)

type SwipeResult struct {
	Swipe          domain.Swipe
	Match          *domain.Match
	ConversationID *uuid.UUID
}

// CreateSwipe stores a swipe and, when it is reciprocal, atomically creates a
// match and a conversation. Swipe + match events are written to the outbox in
// the same transaction.
func (s *Store) CreateSwipe(ctx context.Context, swiperID, targetID uuid.UUID, action string) (*SwipeResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var sw domain.Swipe
	err = tx.QueryRow(ctx, `
		INSERT INTO swipes (swiper_id, target_id, action)
		VALUES ($1, $2, $3)
		ON CONFLICT (swiper_id, target_id) DO NOTHING
		RETURNING id, swiper_id, target_id, action, created_at`,
		swiperID, targetID, action,
	).Scan(&sw.ID, &sw.SwiperID, &sw.TargetID, &sw.Action, &sw.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrAlreadySwiped
	}
	if err != nil {
		return nil, err
	}

	if err := enqueueTx(ctx, tx, "love.events", "swipe.created", sw.ID.String(), map[string]any{
		"swipe_id":   sw.ID,
		"swiper_id":  swiperID,
		"target_id":  targetID,
		"action":     action,
		"created_at": sw.CreatedAt,
	}); err != nil {
		return nil, err
	}

	res := &SwipeResult{Swipe: sw}
	if action != domain.ActionLike && action != domain.ActionSuperlike {
		return res, tx.Commit(ctx)
	}

	var reciprocal bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM swipes
			WHERE swiper_id = $1 AND target_id = $2 AND action IN ('like', 'superlike')
		)`, targetID, swiperID,
	).Scan(&reciprocal); err != nil {
		return nil, err
	}
	if !reciprocal {
		return res, tx.Commit(ctx)
	}

	var match domain.Match
	var conversationID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO matches (user_a, user_b)
		VALUES (least($1::uuid, $2::uuid), greatest($1::uuid, $2::uuid))
		ON CONFLICT (user_a, user_b) DO UPDATE SET unmatched_at = NULL
		RETURNING id, user_a, user_b, created_at`,
		swiperID, targetID,
	).Scan(&match.ID, &match.UserA, &match.UserB, &match.CreatedAt)
	if err != nil {
		return nil, err
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO conversations (match_id)
		VALUES ($1)
		ON CONFLICT (match_id) DO UPDATE SET match_id = EXCLUDED.match_id
		RETURNING id`, match.ID,
	).Scan(&conversationID)
	if err != nil {
		return nil, err
	}
	match.ConversationID = &conversationID
	match.UserA = swiperID
	match.UserB = targetID

	if err := enqueueTx(ctx, tx, "love.events", "match.created", match.ID.String(), map[string]any{
		"match_id":        match.ID,
		"conversation_id": conversationID,
		"user_a":          match.UserA,
		"user_b":          match.UserB,
		"created_at":      match.CreatedAt,
	}); err != nil {
		return nil, err
	}

	res.Match = &match
	res.ConversationID = &conversationID
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Store) GetSwipe(ctx context.Context, swiperID, targetID uuid.UUID) (*domain.Swipe, error) {
	var sw domain.Swipe
	err := s.pool.QueryRow(ctx, `
		SELECT id, swiper_id, target_id, action, created_at
		FROM swipes WHERE swiper_id = $1 AND target_id = $2`, swiperID, targetID,
	).Scan(&sw.ID, &sw.SwiperID, &sw.TargetID, &sw.Action, &sw.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &sw, nil
}

func (s *Store) CountSwipesToday(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM swipes
		WHERE swiper_id = $1 AND action IN ('like', 'superlike')
		  AND created_at >= date_trunc('day', now())`, userID).Scan(&n)
	return n, err
}

func (s *Store) CountSwipesTodayByAction(ctx context.Context, userID uuid.UUID, action string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM swipes
		WHERE swiper_id = $1 AND action = $2
		  AND created_at >= date_trunc('day', now())`, userID, action).Scan(&n)
	return n, err
}

// Rewind removes the most recent swipe if it is still within the rewind window
// and has not produced an active match.
func (s *Store) Rewind(ctx context.Context, userID uuid.UUID, within time.Duration) (*domain.Swipe, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var sw domain.Swipe
	err = tx.QueryRow(ctx, `
		SELECT id, swiper_id, target_id, action, created_at
		FROM swipes
		WHERE swiper_id = $1 AND created_at >= now() - $2::interval
		  AND NOT EXISTS (
		      SELECT 1 FROM matches m
		      WHERE m.unmatched_at IS NULL
		        AND ((m.user_a = $1 AND m.user_b = swipes.target_id) OR (m.user_b = $1 AND m.user_a = swipes.target_id))
		  )
		ORDER BY created_at DESC
		LIMIT 1`, userID, within.String(),
	).Scan(&sw.ID, &sw.SwiperID, &sw.TargetID, &sw.Action, &sw.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM swipes WHERE id = $1`, sw.ID); err != nil {
		return nil, err
	}
	if err := enqueueTx(ctx, tx, "love.events", "swipe.rewound", sw.ID.String(), map[string]any{
		"swipe_id":  sw.ID,
		"swiper_id": sw.SwiperID,
		"target_id": sw.TargetID,
		"action":    sw.Action,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &sw, nil
}

func (s *Store) IsBlocked(ctx context.Context, userA, userB uuid.UUID) (bool, error) {
	var blocked bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM blocks
			WHERE (blocker_id = $1 AND blocked_id = $2) OR (blocker_id = $2 AND blocked_id = $1)
		)`, userA, userB).Scan(&blocked)
	return blocked, err
}
