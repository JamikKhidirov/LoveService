package store

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/domain"
)

// CreateAuthSession registers one logged-in device. Every access token issued
// for that login carries the returned id as its `sid` claim.
func (s *Store) CreateAuthSession(ctx context.Context, sess *domain.AuthSession) (*domain.AuthSession, error) {
	var out domain.AuthSession
	err := s.pool.QueryRow(ctx, `
		INSERT INTO auth_sessions (user_id, platform, device_model, ip, user_agent)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, user_id, platform, device_model, ip, user_agent, created_at, last_seen_at, revoked_at`,
		sess.UserID, sess.Platform, sess.DeviceModel, sess.IP, sess.UserAgent,
	).Scan(&out.ID, &out.UserID, &out.Platform, &out.DeviceModel, &out.IP, &out.UserAgent, &out.CreatedAt, &out.LastSeenAt, &out.RevokedAt)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// AuthSessionActive is checked on every authenticated request when the token
// carries a session id.
func (s *Store) AuthSessionActive(ctx context.Context, userID, sessionID uuid.UUID) (bool, error) {
	var active bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM auth_sessions
			WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL
		)`, sessionID, userID).Scan(&active)
	return active, err
}

// TouchAuthSession refreshes last_seen_at at most once a minute to keep the
// write amplification low while still surfacing recent activity.
func (s *Store) TouchAuthSession(ctx context.Context, sessionID uuid.UUID) {
	if sessionID == uuid.Nil {
		return
	}
	_, _ = s.pool.Exec(ctx, `
		UPDATE auth_sessions SET last_seen_at = now()
		WHERE id = $1 AND last_seen_at < now() - interval '1 minute'`, sessionID)
}

func (s *Store) ListAuthSessions(ctx context.Context, userID uuid.UUID) ([]domain.AuthSession, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, platform, device_model, ip, user_agent, created_at, last_seen_at, revoked_at
		FROM auth_sessions
		WHERE user_id = $1 AND revoked_at IS NULL
		ORDER BY last_seen_at DESC, created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.AuthSession, 0, 6)
	for rows.Next() {
		var sess domain.AuthSession
		if err := rows.Scan(&sess.ID, &sess.UserID, &sess.Platform, &sess.DeviceModel, &sess.IP, &sess.UserAgent,
			&sess.CreatedAt, &sess.LastSeenAt, &sess.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// RevokeAuthSession logs one device out. The refresh tokens bound to the
// session are revoked in the same transaction.
func (s *Store) RevokeAuthSession(ctx context.Context, userID, sessionID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		UPDATE auth_sessions SET revoked_at = now()
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, sessionID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM auth_sessions WHERE id = $1 AND user_id = $2)`, sessionID, userID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return domain.ErrNotFound
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE session_id = $1 AND revoked_at IS NULL`, sessionID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RevokeSessionByRefreshToken is used by POST /auth/logout: the refresh token
// identifies the device session that must be closed.
func (s *Store) RevokeSessionByRefreshToken(ctx context.Context, sessionID uuid.UUID) error {
	if sessionID == uuid.Nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `UPDATE auth_sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, sessionID)
	return err
}

// RevokeAllAuthSessions closes every device session of an account.
func (s *Store) RevokeAllAuthSessions(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE auth_sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	return err
}

// SessionsForCleanup removes sessions that have been revoked for a while.
func (s *Store) DeleteStaleAuthSessions(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM auth_sessions
		WHERE revoked_at IS NOT NULL AND revoked_at < now() - make_interval(secs => $1)`, olderThan.Seconds())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
