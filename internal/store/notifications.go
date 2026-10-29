package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/loveservice/loveservice/internal/domain"
)

func (s *Store) CreateNotification(ctx context.Context, n *domain.Notification) (*domain.Notification, error) {
	if n.Data == nil {
		n.Data = map[string]any{}
	}
	data, err := json.Marshal(n.Data)
	if err != nil {
		return nil, err
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO notifications (user_id, type, title, body, data)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, user_id, type, title, body, data, read_at, created_at`,
		n.UserID, n.Type, n.Title, n.Body, data,
	).Scan(&n.ID, &n.UserID, &n.Type, &n.Title, &n.Body, &data, &n.ReadAt, &n.CreatedAt)
	if err != nil {
		return nil, err
	}
	n.Data = map[string]any{}
	_ = json.Unmarshal(data, &n.Data)
	return n, nil
}

func (s *Store) ListNotifications(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]domain.Notification, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, type, title, body, data, read_at, created_at
		FROM notifications
		WHERE user_id = $1 AND ($2::timestamptz IS NULL OR created_at < $2)
		ORDER BY created_at DESC
		LIMIT $3`, userID, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Notification, 0, limit)
	for rows.Next() {
		var n domain.Notification
		var data []byte
		if err := rows.Scan(&n.ID, &n.UserID, &n.Type, &n.Title, &n.Body, &data, &n.ReadAt, &n.CreatedAt); err != nil {
			return nil, err
		}
		n.Data = map[string]any{}
		_ = json.Unmarshal(data, &n.Data)
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) MarkNotificationRead(ctx context.Context, userID, notificationID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE notifications SET read_at = now()
		WHERE id = $1 AND user_id = $2 AND read_at IS NULL`, notificationID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Idempotent: either already read or not found. Distinguish with a lookup.
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM notifications WHERE id = $1 AND user_id = $2)`, notificationID, userID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return domain.ErrNotFound
		}
	}
	return nil
}

func (s *Store) MarkAllNotificationsRead(ctx context.Context, userID uuid.UUID) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`, userID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) UnreadNotificationCount(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`, userID).Scan(&n)
	return n, err
}

func (s *Store) NotificationsEnabled(ctx context.Context, userID uuid.UUID) (bool, error) {
	var enabled bool
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE((SELECT notifications_enabled FROM user_settings WHERE user_id = $1), true)`, userID).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	return enabled, err
}
