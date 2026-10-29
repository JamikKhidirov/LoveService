package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/loveservice/loveservice/internal/domain"
)

type CreateUserInput struct {
	Email            string
	Phone            *string
	PasswordHash     string
	DisplayName      string
	BirthDate        time.Time
	Gender           string
	PreferredGenders []string
}

func (s *Store) CreateUserWithProfile(ctx context.Context, in CreateUserInput) (*domain.User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var u domain.User
	err = tx.QueryRow(ctx, `
		INSERT INTO users (email, phone, password_hash)
		VALUES ($1, $2, $3)
		RETURNING `+userColumns,
		in.Email, in.Phone, in.PasswordHash,
	).Scan(&u.ID, &u.Email, &u.Phone, &u.PasswordHash, &u.Role, &u.Status, &u.EmailVerifiedAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.ErrEmailTaken
		}
		return nil, fmt.Errorf("insert user: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO profiles (user_id, display_name, birth_date, gender, looking_for)
		VALUES ($1, $2, $3, $4, $5)`,
		u.ID, in.DisplayName, in.BirthDate, in.Gender, preferredLookingFor(in.Gender),
	); err != nil {
		return nil, fmt.Errorf("insert profile: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO preferences (user_id, preferred_genders)
		VALUES ($1, $2)`, u.ID, in.PreferredGenders,
	); err != nil {
		return nil, fmt.Errorf("insert preferences: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_settings (user_id) VALUES ($1)`, u.ID); err != nil {
		return nil, fmt.Errorf("insert settings: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &u, nil
}

func preferredLookingFor(gender string) []string {
	switch gender {
	case domain.GenderMale:
		return []string{"female"}
	case domain.GenderFemale:
		return []string{"male"}
	default:
		return []string{"male", "female", "other"}
	}
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE lower(email) = lower($1)`, email))
}

func (s *Store) GetUserByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
}

func (s *Store) SetEmailVerified(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET email_verified_at = now(), updated_at = now() WHERE id = $1 AND email_verified_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Already verified is fine for idempotency.
		return nil
	}
	return nil
}

func (s *Store) UpdatePassword(ctx context.Context, id uuid.UUID, hash string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, id, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) UpdatePhone(ctx context.Context, id uuid.UUID, phone *string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET phone = $2, updated_at = now() WHERE id = $1`, id, phone)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.Invalid("phone", "this phone number is already in use")
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) UpdateUserStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET status = $2, updated_at = now() WHERE id = $1`, id, status)
	return err
}

func (s *Store) TouchLastSeen(ctx context.Context, id uuid.UUID) {
	_, _ = s.pool.Exec(ctx, `UPDATE profiles SET last_seen_at = now() WHERE user_id = $1`, id)
}

func (s *Store) CreateRefreshToken(ctx context.Context, t *domain.RefreshToken) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at, user_agent, ip)
		VALUES ($1, $2, $3, $4, $5)`,
		t.UserID, t.TokenHash, t.ExpiresAt, t.UserAgent, t.IP,
	)
	return err
}

func (s *Store) GetRefreshTokenByHash(ctx context.Context, hash string) (*domain.RefreshToken, error) {
	var t domain.RefreshToken
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, token_hash, expires_at, revoked_at, user_agent, ip, created_at
		FROM refresh_tokens WHERE token_hash = $1`, hash,
	).Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.RevokedAt, &t.UserAgent, &t.IP, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) RevokeRefreshToken(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, id)
	return err
}

func (s *Store) RevokeUserRefreshTokens(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	return err
}

func (s *Store) DeleteExpiredRefreshTokens(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM refresh_tokens WHERE expires_at < now() - interval '7 days'`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) CreateEmailCode(ctx context.Context, email, purpose, codeHash string, expiresAt time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		UPDATE email_codes SET consumed_at = now()
		WHERE email = $1 AND purpose = $2 AND consumed_at IS NULL`, email, purpose); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO email_codes (email, purpose, code_hash, expires_at)
		VALUES ($1, $2, $3, $4)`, email, purpose, codeHash, expiresAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) GetActiveEmailCode(ctx context.Context, email, purpose string) (*domain.EmailCode, error) {
	var c domain.EmailCode
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, purpose, code_hash, expires_at, consumed_at, attempts, created_at
		FROM email_codes
		WHERE email = $1 AND purpose = $2 AND consumed_at IS NULL AND expires_at > now()
		ORDER BY created_at DESC LIMIT 1`, email, purpose,
	).Scan(&c.ID, &c.Email, &c.Purpose, &c.CodeHash, &c.ExpiresAt, &c.ConsumedAt, &c.Attempts, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) IncrementEmailCodeAttempts(ctx context.Context, id uuid.UUID) (int, error) {
	var attempts int
	err := s.pool.QueryRow(ctx, `UPDATE email_codes SET attempts = attempts + 1 WHERE id = $1 RETURNING attempts`, id).Scan(&attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrNotFound
	}
	return attempts, err
}

func (s *Store) ConsumeEmailCode(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `UPDATE email_codes SET consumed_at = now() WHERE id = $1 AND consumed_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrInvalidToken
	}
	return nil
}

func (s *Store) DeleteAccount(ctx context.Context, userID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE users SET status = 'deleted', updated_at = now() WHERE id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM refresh_tokens WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE photos SET moderation_status = 'rejected' WHERE user_id = $1`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
