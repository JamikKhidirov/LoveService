package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/loveservice/loveservice/internal/domain"
)

type SendGiftResult struct {
	Message     domain.Message `json:"message"`
	Gift        domain.Gift    `json:"gift"`
	SenderCoins int            `json:"sender_coins"`
}

// SendGift debits the sender, credits the recipient, stores the chat message and
// writes the audit rows in one transaction.
func (s *Store) SendGift(ctx context.Context, senderID, conversationID uuid.UUID, gift domain.Gift) (*SendGiftResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var recipientID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT CASE WHEN m.user_a = $2 THEN m.user_b ELSE m.user_a END
		FROM conversations c
		JOIN matches m ON m.id = c.match_id
		WHERE c.id = $1 AND (m.user_a = $2 OR m.user_b = $2) AND m.unmatched_at IS NULL`,
		conversationID, senderID).Scan(&recipientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrConversationClosed
	}
	if err != nil {
		return nil, err
	}

	var senderCoins int
	err = tx.QueryRow(ctx, `
		UPDATE users SET coins = coins - $2, updated_at = now()
		WHERE id = $1 AND coins >= $2
		RETURNING coins`, senderID, gift.Coins).Scan(&senderCoins)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrInsufficientCoins
	}
	if err != nil {
		return nil, err
	}

	var recipientCoins int
	if err := tx.QueryRow(ctx, `
		UPDATE users SET coins = coins + $2, updated_at = now()
		WHERE id = $1
		RETURNING coins`, recipientID, gift.Coins).Scan(&recipientCoins); err != nil {
		return nil, err
	}

	metadata, _ := json.Marshal(map[string]any{
		"gift_code":  gift.Code,
		"gift_name":  gift.Name,
		"gift_emoji": gift.Emoji,
		"coins":      gift.Coins,
	})

	var m domain.Message
	var metadataJSON []byte
	err = tx.QueryRow(ctx, `
		INSERT INTO messages (conversation_id, sender_id, body, type, metadata)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, conversation_id, sender_id, body, type, media_url, metadata, read_at, created_at`,
		conversationID, senderID, gift.Name, domain.MessageGift, metadata,
	).Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Body, &m.Type, &m.MediaURL, &metadataJSON, &m.ReadAt, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(metadataJSON, &m.Metadata)

	if _, err := tx.Exec(ctx, `
		INSERT INTO gift_transactions (conversation_id, message_id, sender_id, recipient_id, gift_code, gift_name, coins)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		conversationID, m.ID, senderID, recipientID, gift.Code, gift.Name, gift.Coins); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO coin_transactions (user_id, amount, kind, reference, balance_after)
		VALUES ($1, $2, $3, $4, $5)`,
		senderID, -gift.Coins, domain.CoinKindGiftSent, gift.Code, senderCoins); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO coin_transactions (user_id, amount, kind, reference, balance_after)
		VALUES ($1, $2, $3, $4, $5)`,
		recipientID, gift.Coins, domain.CoinKindGiftReceived, gift.Code, recipientCoins); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE conversations SET last_message_at = $2 WHERE id = $1`, conversationID, m.CreatedAt); err != nil {
		return nil, err
	}
	if err := enqueueTx(ctx, tx, "love.events", "message.created", m.ID.String(), map[string]any{
		"message_id":      m.ID,
		"conversation_id": conversationID,
		"sender_id":       senderID,
		"recipient_id":    recipientID,
		"body":            gift.Name,
		"type":            domain.MessageGift,
		"gift_code":       gift.Code,
		"coins":           gift.Coins,
		"created_at":      m.CreatedAt,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &SendGiftResult{Message: m, Gift: gift, SenderCoins: senderCoins}, nil
}
