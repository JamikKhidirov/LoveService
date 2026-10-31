package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/loveservice/loveservice/internal/domain"
)

func (s *Store) CreateFaceVerification(ctx context.Context, fv *domain.FaceVerification) (*domain.FaceVerification, error) {
	var out domain.FaceVerification
	err := s.pool.QueryRow(ctx, `
		INSERT INTO face_verifications (user_id, selfie_url, selfie_key, reference_photo_id, score, provider, status, reason, reviewed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, user_id, selfie_url, reference_photo_id, score, provider, status, reason, created_at, reviewed_at`,
		fv.UserID, fv.SelfieURL, fv.SelfieKey, fv.ReferencePhotoID, fv.Score, fv.Provider, fv.Status, fv.Reason, fv.ReviewedAt,
	).Scan(&out.ID, &out.UserID, &out.SelfieURL, &out.ReferencePhotoID, &out.Score, &out.Provider,
		&out.Status, &out.Reason, &out.CreatedAt, &out.ReviewedAt)
	if err != nil {
		return nil, err
	}
	out.SelfieKey = fv.SelfieKey
	return &out, nil
}

func (s *Store) LatestFaceVerification(ctx context.Context, userID uuid.UUID) (*domain.FaceVerification, error) {
	var out domain.FaceVerification
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, selfie_url, reference_photo_id, score, provider, status, reason, created_at, reviewed_at
		FROM face_verifications
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT 1`, userID).Scan(&out.ID, &out.UserID, &out.SelfieURL, &out.ReferencePhotoID, &out.Score,
		&out.Provider, &out.Status, &out.Reason, &out.CreatedAt, &out.ReviewedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Store) MarkFaceVerification(ctx context.Context, id uuid.UUID, status string, score float64, reason string, reviewedAt *time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE face_verifications
		SET status = $2, score = $3, reason = $4, reviewed_at = $5
		WHERE id = $1`, id, status, score, reason, reviewedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
