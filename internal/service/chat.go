package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/cache"
	"github.com/loveservice/loveservice/internal/domain"
	"github.com/loveservice/loveservice/internal/security"
	"github.com/loveservice/loveservice/internal/storage"
	"github.com/loveservice/loveservice/internal/store"
)

type ChatService struct {
	store     *store.Store
	cache     *cache.Cache
	bus       Broadcaster
	log       *slog.Logger
	storage   storage.Storage
	maxUpload int64
}

// ChatOption customises optional chat capabilities without breaking the original
// NewChatService signature used by existing tests.
type ChatOption func(*ChatService)

// WithChatMedia enables uploading photos and short videos inside a chat.
func WithChatMedia(media storage.Storage, maxUploadMB int64) ChatOption {
	return func(s *ChatService) {
		s.storage = media
		if maxUploadMB <= 0 {
			maxUploadMB = 10
		}
		s.maxUpload = maxUploadMB << 20
	}
}

func NewChatService(st *store.Store, c *cache.Cache, bus Broadcaster, log *slog.Logger, opts ...ChatOption) *ChatService {
	s := &ChatService{store: st, cache: c, bus: bus, log: log}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *ChatService) ListConversations(ctx context.Context, userID uuid.UUID) ([]domain.Conversation, error) {
	conversations, err := s.store.ListConversations(ctx, userID)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(conversations))
	for i := range conversations {
		if conversations[i].Peer != nil {
			ids = append(ids, conversations[i].Peer.UserID)
		}
	}
	online := s.cache.OnlineMany(ctx, ids)
	for i := range conversations {
		if conversations[i].Peer == nil {
			continue
		}
		conversations[i].Peer.Age = security.Age(conversations[i].Peer.BirthDate, time.Now().UTC())
		conversations[i].Peer.Online = online[conversations[i].Peer.UserID]
	}
	return conversations, nil
}

func (s *ChatService) GetConversation(ctx context.Context, userID, conversationID uuid.UUID) (*domain.Conversation, error) {
	conversation, err := s.store.GetConversation(ctx, userID, conversationID)
	if err != nil {
		return nil, err
	}
	if conversation.Peer != nil {
		enrichProfile(ctx, s.cache, conversation.Peer)
	}
	return conversation, nil
}

func (s *ChatService) ListMessages(ctx context.Context, userID, conversationID uuid.UUID, before *time.Time, limit int) ([]domain.Message, error) {
	if _, err := s.store.GetConversation(ctx, userID, conversationID); err != nil {
		return nil, err
	}
	return s.store.ListMessages(ctx, conversationID, before, limit)
}

func (s *ChatService) SendMessage(ctx context.Context, userID, conversationID uuid.UUID, body string, mediaURL *string) (*domain.Message, error) {
	if err := security.ValidateMessage(body); err != nil {
		return nil, err
	}
	msgType := domain.MessageText
	if mediaURL != nil && *mediaURL != "" {
		msgType = domain.MessageImage
	}
	message, err := s.store.CreateMessage(ctx, store.CreateMessageInput{
		ConversationID: conversationID,
		SenderID:       userID,
		Body:           body,
		Type:           msgType,
		MediaURL:       mediaURL,
	})
	if err != nil {
		return nil, err
	}
	s.broadcastMessage(ctx, userID, conversationID, message)
	return message, nil
}

// SendMedia uploads a photo or a short video and posts it as a chat message.
func (s *ChatService) SendMedia(ctx context.Context, userID, conversationID uuid.UUID, filename, contentType string, size int64, r io.Reader) (*domain.Message, error) {
	if s.storage == nil {
		return nil, domain.Invalid("file", "chat media uploads are not enabled")
	}
	if _, err := s.store.GetConversation(ctx, userID, conversationID); err != nil {
		return nil, err
	}
	if size <= 0 {
		return nil, domain.Invalid("file", "file is empty")
	}
	if size > s.maxUpload {
		return nil, domain.Invalid("file", fmt.Sprintf("file must be smaller than %d MB", s.maxUpload>>20))
	}
	mediaType, ext, err := detectMedia(filename, contentType)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("chat/%s/%s%s", conversationID.String(), uuid.NewString(), ext)
	url, err := s.storage.Put(ctx, key, r, size, contentType)
	if err != nil {
		return nil, fmt.Errorf("store chat media: %w", err)
	}
	msgType := domain.MessageImage
	if mediaType == domain.MediaVideo {
		msgType = domain.MessageVideo
	}
	message, err := s.store.CreateMessage(ctx, store.CreateMessageInput{
		ConversationID: conversationID,
		SenderID:       userID,
		Body:           "",
		Type:           msgType,
		MediaURL:       &url,
		Metadata:       map[string]any{"storage_key": key, "content_type": contentType},
	})
	if err != nil {
		_ = s.storage.Delete(ctx, key)
		return nil, err
	}
	s.broadcastMessage(ctx, userID, conversationID, message)
	return message, nil
}

// GiftMessage is the transport-neutral result of sending a virtual gift.
type GiftMessage struct {
	Message     domain.Message `json:"message"`
	Gift        domain.Gift    `json:"gift"`
	SenderCoins int            `json:"sender_coins"`
}

// SendGift spends coins on a chat gift. The sender is debited and the recipient
// credited atomically by the store.
func (s *ChatService) SendGift(ctx context.Context, userID, conversationID uuid.UUID, code string) (*GiftMessage, error) {
	gift, ok := domain.FindGift(code)
	if !ok {
		return nil, domain.Invalid("gift", "unknown gift code")
	}
	res, err := s.store.SendGift(ctx, userID, conversationID, gift)
	if err != nil {
		return nil, err
	}
	if s.bus != nil {
		peerID, err := s.store.ConversationPeer(ctx, userID, conversationID)
		if err == nil {
			payload := map[string]any{
				"conversation_id": conversationID,
				"message":         res.Message,
				"gift":            res.Gift,
			}
			s.bus.SendToUser(userID, "message.new", payload)
			s.bus.SendToUser(userID, "gift.sent", payload)
			s.bus.SendToUser(peerID, "message.new", payload)
			s.bus.SendToUser(peerID, "gift.received", payload)
		}
	}
	return &GiftMessage{Message: res.Message, Gift: res.Gift, SenderCoins: res.SenderCoins}, nil
}

// GiftCatalog exposes the fixed price list.
func (s *ChatService) GiftCatalog() []domain.Gift {
	out := make([]domain.Gift, len(domain.GiftCatalog))
	copy(out, domain.GiftCatalog)
	return out
}

func (s *ChatService) broadcastMessage(ctx context.Context, senderID, conversationID uuid.UUID, message *domain.Message) {
	if s.bus == nil || message == nil {
		return
	}
	payload := map[string]any{
		"conversation_id": conversationID,
		"message":         message,
	}
	s.bus.SendToUser(senderID, "message.new", payload)
	if peerID, err := s.store.ConversationPeer(ctx, senderID, conversationID); err == nil {
		s.bus.SendToUser(peerID, "message.new", payload)
	}
}

func (s *ChatService) MarkRead(ctx context.Context, userID, conversationID uuid.UUID) ([]uuid.UUID, time.Time, error) {
	senders, readAt, err := s.store.MarkMessagesRead(ctx, conversationID, userID)
	if err != nil {
		return nil, time.Time{}, err
	}
	if s.bus != nil {
		for _, sender := range senders {
			s.bus.SendToUser(sender, "message.read", map[string]any{
				"conversation_id": conversationID,
				"reader_id":       userID,
				"read_at":         readAt,
			})
		}
	}
	return senders, readAt, nil
}

func (s *ChatService) Typing(ctx context.Context, userID, conversationID uuid.UUID) error {
	peerID, err := s.store.ConversationPeer(ctx, userID, conversationID)
	if err != nil {
		return err
	}
	if s.cache.Available() {
		allowed, _, err := s.cache.Allow(ctx, fmt.Sprintf("typing:%s:%s", userID, conversationID), 1, time.Second)
		if err == nil && !allowed {
			return nil
		}
	}
	profile, err := s.store.GetProfile(ctx, userID)
	if err != nil {
		return err
	}
	if s.bus != nil {
		s.bus.SendToUser(peerID, "typing", map[string]any{
			"conversation_id": conversationID,
			"user_id":         userID,
			"display_name":    profile.DisplayName,
		})
	}
	return nil
}

func (s *ChatService) UnreadCount(ctx context.Context, userID uuid.UUID) (int, error) {
	stats, err := s.store.GetStats(ctx, userID)
	if err != nil {
		return 0, err
	}
	return stats.UnreadMessages, nil
}
