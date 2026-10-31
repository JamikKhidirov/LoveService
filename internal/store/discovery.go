package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/loveservice/loveservice/internal/domain"
)

// distanceExpr is the haversine distance in kilometers between ($2,$3) and a
// profile row p. It is precise enough for discovery and avoids a PostGIS
// dependency while still using an index-friendly bounding-box prefilter.
const distanceExpr = `(6371.0088 * 2 * asin(sqrt(
    power(sin(radians(p.lat - $2) / 2), 2) +
    cos(radians($2)) * cos(radians(p.lat)) * power(sin(radians(p.lng - $3) / 2), 2)
)))`

const discoverWhere = `
    p.user_id <> $1
    AND u.status = 'active'
    AND COALESCE(st.discoverable, true) = true
    AND p.lat IS NOT NULL AND p.lng IS NOT NULL
    AND p.gender = ANY($6)
    AND date_part('year', age(current_date, p.birth_date)) BETWEEN $4 AND $5
    AND NOT EXISTS (SELECT 1 FROM swipes sw WHERE sw.swiper_id = $1 AND sw.target_id = p.user_id)
    AND NOT EXISTS (SELECT 1 FROM swipes back WHERE back.swiper_id = p.user_id AND back.target_id = $1 AND back.action = 'dislike')
    AND NOT EXISTS (
        SELECT 1 FROM blocks b
        WHERE (b.blocker_id = $1 AND b.blocked_id = p.user_id)
           OR (b.blocker_id = p.user_id AND b.blocked_id = $1)
    )`

func (s *Store) Discover(ctx context.Context, q domain.DiscoveryQuery) ([]domain.Profile, int, error) {
	if q.Limit <= 0 {
		q.Limit = 20
	}
	if q.Limit > 50 {
		q.Limit = 50
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	if q.RadiusKM <= 0 {
		q.RadiusKM = 50
	}
	if len(q.Genders) == 0 {
		q.Genders = []string{domain.GenderFemale, domain.GenderMale, domain.GenderOther}
	}
	if q.MinAge < 18 {
		q.MinAge = 18
	}
	if q.MaxAge < q.MinAge {
		q.MaxAge = q.MinAge
	}
	switch q.Sort {
	case domain.DiscoverySortDistance, domain.DiscoverySortRating:
	default:
		q.Sort = domain.DiscoverySortBalanced
	}

	query := `
WITH candidates AS (
    SELECT p.user_id, ` + distanceExpr + ` AS distance_km, u.elo_rating AS elo
    FROM profiles p
    JOIN users u ON u.id = p.user_id
    LEFT JOIN user_settings st ON st.user_id = p.user_id
    WHERE ` + discoverWhere + `
),
filtered AS (
    SELECT user_id, distance_km, elo FROM candidates WHERE distance_km <= $7
)
SELECT ` + profileFields + `, f.distance_km, count(*) OVER() AS total
FROM filtered f
JOIN profiles p ON p.user_id = f.user_id
ORDER BY
    CASE $10::text
        WHEN 'rating'   THEN -f.elo
        WHEN 'distance' THEN f.distance_km
        ELSE (f.distance_km / NULLIF($7, 0)) - 0.35 * (f.elo / 2000.0)
    END ASC,
    f.distance_km ASC,
    p.user_id ASC
LIMIT $8 OFFSET $9`

	rows, err := s.pool.Query(ctx, query,
		q.ViewerID, q.Lat, q.Lng, q.MinAge, q.MaxAge, q.Genders, q.RadiusKM, q.Limit, q.Offset, q.Sort,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]domain.Profile, 0, q.Limit)
	total := 0
	for rows.Next() {
		var distance float64
		var rowTotal int
		p, err := scanProfileAnd(rows, &distance, &rowTotal)
		if err != nil {
			return nil, 0, err
		}
		p.DistanceKM = &distance
		total = rowTotal
		out = append(out, *p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// CountCandidates is a cheap total for the current radius without loading rows.
func (s *Store) CountCandidates(ctx context.Context, q domain.DiscoveryQuery) (int, error) {
	if len(q.Genders) == 0 {
		q.Genders = []string{domain.GenderFemale, domain.GenderMale, domain.GenderOther}
	}
	if q.MaxAge < q.MinAge {
		q.MaxAge = q.MinAge
	}
	query := `
SELECT count(*) FROM (
    SELECT p.user_id, ` + distanceExpr + ` AS distance_km
    FROM profiles p
    JOIN users u ON u.id = p.user_id
    LEFT JOIN user_settings st ON st.user_id = p.user_id
    WHERE ` + discoverWhere + `
) c WHERE c.distance_km <= $7`
	var n int
	err := s.pool.QueryRow(ctx, query,
		q.ViewerID, q.Lat, q.Lng, q.MinAge, q.MaxAge, q.Genders, q.RadiusKM,
	).Scan(&n)
	return n, err
}

func (s *Store) ListIncomingLikes(ctx context.Context, userID uuid.UUID, limit int) ([]domain.IncomingLike, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
SELECT `+profileFields+`, s.action, s.created_at,
       EXISTS (
           SELECT 1 FROM matches m
           WHERE m.unmatched_at IS NULL
             AND ((m.user_a = s.swiper_id AND m.user_b = s.target_id) OR (m.user_a = s.target_id AND m.user_b = s.swiper_id))
       ) AS matched
FROM swipes s
JOIN profiles p ON p.user_id = s.swiper_id
JOIN users u ON u.id = s.swiper_id
WHERE s.target_id = $1
  AND s.action IN ('like', 'superlike')
  AND u.status = 'active'
  AND NOT EXISTS (SELECT 1 FROM swipes mine WHERE mine.swiper_id = $1 AND mine.target_id = s.swiper_id)
  AND NOT EXISTS (
      SELECT 1 FROM blocks b
      WHERE (b.blocker_id = $1 AND b.blocked_id = s.swiper_id)
         OR (b.blocker_id = s.swiper_id AND b.blocked_id = $1)
  )
ORDER BY s.created_at DESC
LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.IncomingLike, 0, limit)
	for rows.Next() {
		var item domain.IncomingLike
		p, err := scanProfileAnd(rows, &item.Action, &item.CreatedAt, &item.Matched)
		if err != nil {
			return nil, err
		}
		item.Profile = *p
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListSentLikes(ctx context.Context, userID uuid.UUID, limit int) ([]domain.IncomingLike, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
SELECT `+profileFields+`, s.action, s.created_at,
       EXISTS (
           SELECT 1 FROM swipes them
           WHERE them.swiper_id = s.target_id AND them.target_id = $1 AND them.action IN ('like', 'superlike')
       ) AS matched
FROM swipes s
JOIN profiles p ON p.user_id = s.target_id
JOIN users u ON u.id = s.target_id
WHERE s.swiper_id = $1
  AND s.action IN ('like', 'superlike')
  AND u.status = 'active'
ORDER BY s.created_at DESC
LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.IncomingLike, 0, limit)
	for rows.Next() {
		var item domain.IncomingLike
		p, err := scanProfileAnd(rows, &item.Action, &item.CreatedAt, &item.Matched)
		if err != nil {
			return nil, err
		}
		item.Profile = *p
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetStats(ctx context.Context, userID uuid.UUID) (*domain.Stats, error) {
	var st domain.Stats
	err := s.pool.QueryRow(ctx, `
SELECT
  (SELECT count(*) FROM swipes s
     JOIN users u ON u.id = s.swiper_id
     WHERE s.target_id = $1 AND s.action IN ('like', 'superlike') AND u.status = 'active'
       AND NOT EXISTS (SELECT 1 FROM swipes mine WHERE mine.swiper_id = $1 AND mine.target_id = s.swiper_id)),
  (SELECT count(*) FROM matches m WHERE m.unmatched_at IS NULL AND (m.user_a = $1 OR m.user_b = $1)),
  (SELECT count(*) FROM messages msg
     JOIN conversations c ON c.id = msg.conversation_id
     JOIN matches m ON m.id = c.match_id
     WHERE msg.sender_id <> $1 AND msg.read_at IS NULL AND m.unmatched_at IS NULL
       AND (m.user_a = $1 OR m.user_b = $1)),
  (SELECT count(*) FROM notifications n WHERE n.user_id = $1 AND n.read_at IS NULL)`,
		userID,
	).Scan(&st.IncomingLikes, &st.Matches, &st.UnreadMessages, &st.UnreadNotifications)
	if errors.Is(err, pgx.ErrNoRows) {
		return &st, nil
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}
