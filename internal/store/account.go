package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/loveservice/loveservice/internal/domain"
)

func (s *Store) BlockUser(ctx context.Context, blockerID, blockedID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, blockerID, blockedID)
	if err != nil {
		if isForeignKeyViolation(err) {
			return domain.ErrNotFound
		}
		return err
	}
	// Blocking also hides an existing conversation.
	_, err = s.pool.Exec(ctx, `
		UPDATE matches SET unmatched_at = now()
		WHERE unmatched_at IS NULL
		  AND ((user_a = $1 AND user_b = $2) OR (user_a = $2 AND user_b = $1))`, blockerID, blockedID)
	return err
}

func (s *Store) UnblockUser(ctx context.Context, blockerID, blockedID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM blocks WHERE blocker_id = $1 AND blocked_id = $2`, blockerID, blockedID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) ListBlocked(ctx context.Context, userID uuid.UUID) ([]domain.Profile, error) {
	rows, err := s.pool.Query(ctx, `
SELECT `+profileFields+`
FROM blocks b
JOIN profiles p ON p.user_id = b.blocked_id
WHERE b.blocker_id = $1
ORDER BY b.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Profile{}
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) CreateReport(ctx context.Context, reporterID, targetID uuid.UUID, reason, details string) (*domain.Report, error) {
	var r domain.Report
	err := s.pool.QueryRow(ctx, `
		INSERT INTO reports (reporter_id, target_id, reason, details)
		VALUES ($1, $2, $3, $4)
		RETURNING id, reporter_id, target_id, reason, details, status, created_at`,
		reporterID, targetID, reason, details,
	).Scan(&r.ID, &r.ReporterID, &r.TargetID, &r.Reason, &r.Details, &r.Status, &r.CreatedAt)
	if err != nil {
		if isForeignKeyViolation(err) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &r, nil
}

func (s *Store) UpsertDevice(ctx context.Context, userID uuid.UUID, platform, token string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO devices (user_id, platform, token)
		VALUES ($1, $2, $3)
		ON CONFLICT (token) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			platform = EXCLUDED.platform`,
		userID, platform, token)
	return err
}

func (s *Store) DeleteDevice(ctx context.Context, userID uuid.UUID, token string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM devices WHERE user_id = $1 AND token = $2`, userID, token)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) ListDeviceTokens(ctx context.Context, userID uuid.UUID) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT token FROM devices WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			return nil, err
		}
		out = append(out, token)
	}
	return out, rows.Err()
}

func (s *Store) UserExists(ctx context.Context, userID uuid.UUID) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND status = 'active')`, userID).Scan(&ok)
	return ok, err
}

func (s *Store) TargetProfileVisible(ctx context.Context, viewerID, targetID uuid.UUID) (*domain.Profile, error) {
	p, err := s.GetProfile(ctx, targetID)
	if err != nil {
		return nil, err
	}
	blocked, err := s.IsBlocked(ctx, viewerID, targetID)
	if err != nil {
		return nil, err
	}
	if blocked {
		return nil, domain.ErrBlocked
	}
	user, err := s.GetUserByID(ctx, targetID)
	if err != nil {
		return nil, err
	}
	if user.Status != domain.StatusActive {
		return nil, domain.ErrNotFound
	}
	return p, nil
}

func (s *Store) LastInsertID(ctx context.Context) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, domain.ErrNotFound
	}
	return id, err
}
