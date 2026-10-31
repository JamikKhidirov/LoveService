package store

import (
	"context"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/domain"
)

// RecordProfileView stores at most one view per viewer/target/day, which keeps
// the table small and the statistic meaningful.
func (s *Store) RecordProfileView(ctx context.Context, viewerID, targetID uuid.UUID) error {
	if viewerID == targetID {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO profile_views (viewer_id, target_id)
		VALUES ($1, $2)
		ON CONFLICT (viewer_id, target_id, viewed_on) DO NOTHING`, viewerID, targetID)
	return err
}

func (s *Store) GetProfileStats(ctx context.Context, userID uuid.UUID) (*domain.ProfileStats, error) {
	var st domain.ProfileStats
	err := s.pool.QueryRow(ctx, `
SELECT
  (SELECT count(*) FROM profile_views v WHERE v.target_id = $1),
  (SELECT count(*) FROM profile_views v WHERE v.target_id = $1 AND v.viewed_on >= current_date - 6),
  (SELECT count(*) FROM swipes s
     JOIN users u ON u.id = s.swiper_id
     WHERE s.target_id = $1 AND s.action IN ('like', 'superlike') AND u.status = 'active'
       AND s.created_at >= now() - interval '7 days'),
  (SELECT count(*) FROM swipes s
     JOIN users u ON u.id = s.swiper_id
     WHERE s.target_id = $1 AND s.action IN ('like', 'superlike') AND u.status = 'active'),
  (SELECT count(*) FROM swipes s
     JOIN users u ON u.id = s.swiper_id
     WHERE s.target_id = $1 AND s.action IN ('like', 'superlike') AND u.status = 'active'
       AND NOT EXISTS (SELECT 1 FROM swipes mine WHERE mine.swiper_id = $1 AND mine.target_id = s.swiper_id)),
  (SELECT count(*) FROM matches m
     WHERE (m.user_a = $1 OR m.user_b = $1) AND m.created_at >= now() - interval '7 days'),
  (SELECT count(*) FROM matches m
     WHERE (m.user_a = $1 OR m.user_b = $1) AND m.unmatched_at IS NULL)`,
		userID,
	).Scan(&st.ProfileViewsTotal, &st.ProfileViewsWeek, &st.LikesReceivedWeek,
		&st.LikesReceivedTotal, &st.LikesYou, &st.MatchesWeek, &st.MatchesTotal)
	if err != nil {
		return nil, err
	}
	return &st, nil
}
