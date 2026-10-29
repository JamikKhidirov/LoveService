package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/loveservice/loveservice/internal/domain"
)

const conversationFields = `c.id, c.match_id, c.created_at, c.last_message_at,
       (SELECT count(*) FROM messages msg
        WHERE msg.conversation_id = c.id AND msg.sender_id <> $1 AND msg.read_at IS NULL) AS unread,
       COALESCE(lm.id, '00000000-0000-0000-0000-000000000000'::uuid),
       COALESCE(lm.sender_id, '00000000-0000-0000-0000-000000000000'::uuid),
       COALESCE(lm.body, ''),
       COALESCE(lm.type, 'text'),
       lm.media_url,
       lm.read_at,
       COALESCE(lm.created_at, c.created_at)`

func (s *Store) ListConversations(ctx context.Context, userID uuid.UUID) ([]domain.Conversation, error) {
	rows, err := s.pool.Query(ctx, `
SELECT `+profileFields+`, `+conversationFields+`
FROM conversations c
JOIN matches m ON m.id = c.match_id
JOIN profiles p ON p.user_id = CASE WHEN m.user_a = $1 THEN m.user_b ELSE m.user_a END
JOIN users u ON u.id = p.user_id
LEFT JOIN LATERAL (
    SELECT * FROM messages WHERE conversation_id = c.id ORDER BY created_at DESC LIMIT 1
) lm ON true
WHERE (m.user_a = $1 OR m.user_b = $1)
  AND m.unmatched_at IS NULL
  AND u.status = 'active'
ORDER BY COALESCE(c.last_message_at, c.created_at) DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Conversation, 0, 20)
	for rows.Next() {
		conv, err := scanConversationAnd(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *conv)
	}
	return out, rows.Err()
}

func (s *Store) GetConversation(ctx context.Context, userID, conversationID uuid.UUID) (*domain.Conversation, error) {
	row := s.pool.QueryRow(ctx, `
SELECT `+profileFields+`, `+conversationFields+`
FROM conversations c
JOIN matches m ON m.id = c.match_id
JOIN profiles p ON p.user_id = CASE WHEN m.user_a = $1 THEN m.user_b ELSE m.user_a END
JOIN users u ON u.id = p.user_id
LEFT JOIN LATERAL (
    SELECT * FROM messages WHERE conversation_id = c.id ORDER BY created_at DESC LIMIT 1
) lm ON true
WHERE c.id = $2
  AND (m.user_a = $1 OR m.user_b = $1)
  AND m.unmatched_at IS NULL
  AND u.status = 'active'`, userID, conversationID)
	conv, err := scanConversationAnd(row)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return conv, nil
}

func scanConversationAnd(row pgx.Row) (*domain.Conversation, error) {
	var c domain.Conversation
	var lastID, lastSender uuid.UUID
	var lastBody, lastType string
	var lastMediaURL *string
	var lastReadAt *time.Time
	var lastCreated time.Time

	p, err := scanProfileAnd(row,
		&c.ID, &c.MatchID, &c.CreatedAt, &c.LastMessageAt, &c.UnreadCount,
		&lastID, &lastSender, &lastBody, &lastType, &lastMediaURL, &lastReadAt, &lastCreated,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Peer = p
	if lastID != uuid.Nil {
		c.LastMessage = &domain.Message{
			ID:             lastID,
			ConversationID: c.ID,
			SenderID:       lastSender,
			Body:           lastBody,
			Type:           lastType,
			MediaURL:       lastMediaURL,
			ReadAt:         lastReadAt,
			CreatedAt:      lastCreated,
		}
	}
	return &c, nil
}

func (s *Store) ConversationPeer(ctx context.Context, userID, conversationID uuid.UUID) (uuid.UUID, error) {
	var peer uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT CASE WHEN m.user_a = $1 THEN m.user_b ELSE m.user_a END
		FROM conversations c
		JOIN matches m ON m.id = c.match_id
		WHERE c.id = $2 AND (m.user_a = $1 OR m.user_b = $1) AND m.unmatched_at IS NULL`,
		userID, conversationID,
	).Scan(&peer)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, domain.ErrConversationClosed
	}
	return peer, err
}

func (s *Store) ListMessages(ctx context.Context, conversationID uuid.UUID, before *time.Time, limit int) ([]domain.Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, conversation_id, sender_id, body, type, media_url, read_at, created_at
		FROM messages
		WHERE conversation_id = $1
		  AND ($2::timestamptz IS NULL OR created_at < $2)
		ORDER BY created_at DESC
		LIMIT $3`, conversationID, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Message, 0, limit)
	for rows.Next() {
		var m domain.Message
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Body, &m.Type, &m.MediaURL, &m.ReadAt, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Return ascending for convenient chat rendering.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

type CreateMessageInput struct {
	ConversationID uuid.UUID
	SenderID       uuid.UUID
	Body           string
	Type           string
	MediaURL       *string
}

func (s *Store) CreateMessage(ctx context.Context, in CreateMessageInput) (*domain.Message, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var peerID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT CASE WHEN m.user_a = $2 THEN m.user_b ELSE m.user_a END
		FROM conversations c
		JOIN matches m ON m.id = c.match_id
		WHERE c.id = $1 AND (m.user_a = $2 OR m.user_b = $2) AND m.unmatched_at IS NULL`,
		in.ConversationID, in.SenderID,
	).Scan(&peerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrConversationClosed
	}
	if err != nil {
		return nil, err
	}

	var m domain.Message
	err = tx.QueryRow(ctx, `
		INSERT INTO messages (conversation_id, sender_id, body, type, media_url)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, conversation_id, sender_id, body, type, media_url, read_at, created_at`,
		in.ConversationID, in.SenderID, in.Body, in.Type, in.MediaURL,
	).Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Body, &m.Type, &m.MediaURL, &m.ReadAt, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE conversations SET last_message_at = $2 WHERE id = $1`, in.ConversationID, m.CreatedAt); err != nil {
		return nil, err
	}

	preview := []rune(m.Body)
	if len(preview) > 120 {
		preview = preview[:120]
	}
	if err := enqueueTx(ctx, tx, "love.events", "message.created", m.ID.String(), map[string]any{
		"message_id":      m.ID,
		"conversation_id": m.ConversationID,
		"sender_id":       m.SenderID,
		"recipient_id":    peerID,
		"body":            string(preview),
		"type":            m.Type,
		"created_at":      m.CreatedAt,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) MarkMessagesRead(ctx context.Context, conversationID, userID uuid.UUID) ([]uuid.UUID, time.Time, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM conversations c
			JOIN matches m ON m.id = c.match_id
			WHERE c.id = $1 AND (m.user_a = $2 OR m.user_b = $2) AND m.unmatched_at IS NULL
		)`, conversationID, userID,
	).Scan(&exists); err != nil {
		return nil, time.Time{}, err
	}
	if !exists {
		return nil, time.Time{}, domain.ErrConversationClosed
	}

	rows, err := tx.Query(ctx, `
		UPDATE messages SET read_at = now()
		WHERE conversation_id = $1 AND sender_id <> $2 AND read_at IS NULL
		RETURNING sender_id`, conversationID, userID)
	if err != nil {
		return nil, time.Time{}, err
	}
	seen := map[uuid.UUID]struct{}{}
	senders := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, time.Time{}, err
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			senders = append(senders, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, err
	}

	// Insert a read receipt event for the peer(s).
	readAt := time.Now().UTC()
	for _, sender := range senders {
		if err := enqueueTx(ctx, tx, "love.events", "message.read", conversationID.String(), map[string]any{
			"conversation_id": conversationID,
			"reader_id":       userID,
			"sender_id":       sender,
			"read_at":         readAt,
		}); err != nil {
			return nil, time.Time{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, time.Time{}, err
	}
	return senders, readAt, nil
}
