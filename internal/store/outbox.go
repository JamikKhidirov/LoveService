package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/loveservice/loveservice/internal/domain"
)

func enqueueTx(ctx context.Context, tx pgx.Tx, topic, eventType, aggregateID string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal outbox payload: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (topic, event_type, aggregate_id, payload)
		VALUES ($1, $2, $3, $4)`, topic, eventType, aggregateID, body)
	if err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}

// Enqueue stages an integration event in the transactional outbox.
func (s *Store) Enqueue(ctx context.Context, topic, eventType, aggregateID string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO outbox_events (topic, event_type, aggregate_id, payload)
		VALUES ($1, $2, $3, $4)`, topic, eventType, aggregateID, body)
	return err
}

func (s *Store) FetchPendingOutbox(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, topic, event_type, aggregate_id, payload, created_at, published_at, attempts
		FROM outbox_events
		WHERE published_at IS NULL
		ORDER BY created_at
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.OutboxEvent, 0, limit)
	for rows.Next() {
		var e domain.OutboxEvent
		if err := rows.Scan(&e.ID, &e.Topic, &e.EventType, &e.AggregateID, &e.Payload, &e.CreatedAt, &e.PublishedAt, &e.Attempts); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) MarkOutboxPublished(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE outbox_events SET published_at = now(), attempts = attempts + 1, last_error = '' WHERE id = $1`, id)
	return err
}

func (s *Store) MarkOutboxFailed(ctx context.Context, id uuid.UUID, errText string) error {
	if len(errText) > 1000 {
		errText = errText[:1000]
	}
	_, err := s.pool.Exec(ctx, `UPDATE outbox_events SET attempts = attempts + 1, last_error = $2 WHERE id = $1`, id, errText)
	return err
}

func (s *Store) CleanupOutbox(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM outbox_events WHERE published_at IS NOT NULL AND published_at < now() - $1::interval`, olderThan.String())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
