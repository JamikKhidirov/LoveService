package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/cache"
	"github.com/loveservice/loveservice/internal/domain"
	"github.com/loveservice/loveservice/internal/security"
	"github.com/loveservice/loveservice/internal/store"
)

type ChatService struct {
	store *store.Store
	cache *cache.Cache
	bus   Broadcaster
	log   *slog.Logger
}

func NewChatService(st *store.Store, c *cache.Cache, bus Broadcaster, log *slog.Logger) *ChatService {
	return &ChatService{store: st, cache: c, bus: bus, log: log}
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
	peerID, err := s.store.ConversationPeer(ctx, userID, conversationID)
	if err == nil && s.bus != nil {
		s.bus.SendToUser(userID, "message.new", map[string]any{
			"conversation_id": conversationID,
			"message":         message,
		})
		s.bus.SendToUser(peerID, "message.new", map[string]any{
			"conversation_id": conversationID,
			"message":         message,
		})
	}
	return message, nil
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
