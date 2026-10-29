package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/domain"
	"github.com/loveservice/loveservice/internal/events"
	"github.com/loveservice/loveservice/internal/store"
)

type NotificationService struct {
	store *store.Store
	bus   Broadcaster
	log   *slog.Logger
}

func NewNotificationService(st *store.Store, bus Broadcaster, log *slog.Logger) *NotificationService {
	return &NotificationService{store: st, bus: bus, log: log}
}

func (s *NotificationService) List(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]domain.Notification, error) {
	return s.store.ListNotifications(ctx, userID, before, limit)
}

func (s *NotificationService) MarkRead(ctx context.Context, userID, notificationID uuid.UUID) error {
	return s.store.MarkNotificationRead(ctx, userID, notificationID)
}

func (s *NotificationService) MarkAllRead(ctx context.Context, userID uuid.UUID) (int64, error) {
	return s.store.MarkAllNotificationsRead(ctx, userID)
}

func (s *NotificationService) UnreadCount(ctx context.Context, userID uuid.UUID) (int, error) {
	return s.store.UnreadNotificationCount(ctx, userID)
}

// HandleEvent is the event-stream consumer. It creates in-app notifications and
// fans them out over the websocket gateway.
func (s *NotificationService) HandleEvent(ctx context.Context, event events.Event) error {
	switch event.Type {
	case "user.registered":
		var p struct {
			UserID uuid.UUID `json:"user_id"`
			Name   string    `json:"name"`
		}
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return err
		}
		return s.notify(ctx, p.UserID, "welcome", "Добро пожаловать в LoveService! 💖",
			"Заполните профиль, добавьте фото и начните знакомиться с людьми рядом.",
			map[string]any{"screen": "profile"})
	case "swipe.created":
		var p struct {
			SwipeID  uuid.UUID `json:"swipe_id"`
			SwiperID uuid.UUID `json:"swiper_id"`
			TargetID uuid.UUID `json:"target_id"`
			Action   string    `json:"action"`
		}
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return err
		}
		if p.Action != domain.ActionLike && p.Action != domain.ActionSuperlike {
			return nil
		}
		// A reciprocal like already produces a much better "match" notification.
		if matched, _ := s.store.AreMatched(ctx, p.SwiperID, p.TargetID); matched {
			return nil
		}
		title := "Новый лайк 💗"
		body := "Кому-то понравился ваш профиль. Загляните в раздел лайков."
		if p.Action == domain.ActionSuperlike {
			title = "Суперлайк ⭐"
			body = "Кто-то выделил ваш профиль суперлайком!"
		}
		return s.notify(ctx, p.TargetID, "like", title, body, map[string]any{
			"swipe_id": p.SwipeID,
			"action":   p.Action,
			"screen":   "likes",
		})
	case "match.created":
		var p struct {
			MatchID        uuid.UUID `json:"match_id"`
			ConversationID uuid.UUID `json:"conversation_id"`
			UserA          uuid.UUID `json:"user_a"`
			UserB          uuid.UUID `json:"user_b"`
		}
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return err
		}
		for _, pair := range [][2]uuid.UUID{{p.UserA, p.UserB}, {p.UserB, p.UserA}} {
			userID, peerID := pair[0], pair[1]
			peer, err := s.store.GetProfile(ctx, peerID)
			name := "Новый человек"
			var photo string
			if err == nil {
				name = peer.DisplayName
				for _, ph := range peer.Photos {
					if ph.IsPrimary {
						photo = ph.URL
						break
					}
				}
				if photo == "" && len(peer.Photos) > 0 {
					photo = peer.Photos[0].URL
				}
			}
			data := map[string]any{
				"match_id":        p.MatchID,
				"conversation_id": p.ConversationID,
				"peer_id":         peerID,
				"peer_name":       name,
				"screen":          "chat",
			}
			if photo != "" {
				data["peer_photo"] = photo
			}
			if s.bus != nil {
				s.bus.SendToUser(userID, "match.new", data)
			}
			if err := s.notify(ctx, userID, "match", "Это взаимно! 💞", "Вы понравились друг другу. Напишите первым!", data); err != nil {
				s.log.Warn("could not create match notification", "user_id", userID, "error", err)
			}
		}
		return nil
	case "message.created":
		var p struct {
			MessageID      uuid.UUID `json:"message_id"`
			ConversationID uuid.UUID `json:"conversation_id"`
			SenderID       uuid.UUID `json:"sender_id"`
			RecipientID    uuid.UUID `json:"recipient_id"`
			Body           string    `json:"body"`
			Type           string    `json:"type"`
		}
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return err
		}
		sender, _ := s.store.GetProfile(ctx, p.SenderID)
		name := "Новое сообщение"
		if sender != nil {
			name = sender.DisplayName
		}
		body := p.Body
		if p.Type != domain.MessageText {
			body = "Вам отправили вложение"
		}
		return s.notify(ctx, p.RecipientID, "message", name, body, map[string]any{
			"message_id":      p.MessageID,
			"conversation_id": p.ConversationID,
			"sender_id":       p.SenderID,
			"screen":          "chat",
		})
	case "user.email_verified":
		var p struct {
			UserID uuid.UUID `json:"user_id"`
		}
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return err
		}
		return s.notify(ctx, p.UserID, "system", "Почта подтверждена ✅", "Ваш аккаунт стал безопаснее.", map[string]any{"screen": "settings"})
	default:
		return nil
	}
}

func (s *NotificationService) notify(ctx context.Context, userID uuid.UUID, kind, title, body string, data map[string]any) error {
	if userID == uuid.Nil {
		return nil
	}
	enabled, err := s.store.NotificationsEnabled(ctx, userID)
	if err != nil {
		s.log.Warn("could not read notification settings", "user_id", userID, "error", err)
	}
	if !enabled {
		return nil
	}
	n, err := s.store.CreateNotification(ctx, &domain.Notification{
		UserID: userID, Type: kind, Title: title, Body: body, Data: data,
	})
	if err != nil {
		return err
	}
	if s.bus != nil {
		s.bus.SendToUser(userID, "notification.new", n)
	}
	return nil
}
