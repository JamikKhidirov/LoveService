package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/loveservice/loveservice/internal/domain"
	"github.com/loveservice/loveservice/internal/security"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 90 * time.Second
	pingPeriod     = 30 * time.Second
	maxMessageSize = 16 * 1024
	sendBuffer     = 64
)

type ChatService interface {
	SendMessage(ctx context.Context, userID, conversationID uuid.UUID, body string, mediaURL *string) (*domain.Message, error)
	MarkRead(ctx context.Context, userID, conversationID uuid.UUID) ([]uuid.UUID, time.Time, error)
	Typing(ctx context.Context, userID, conversationID uuid.UUID) error
}

type TokenParser interface {
	ParseAccess(raw string) (*security.Claims, error)
}

type UserChecker interface {
	UserExists(ctx context.Context, userID uuid.UUID) (bool, error)
}

type Handler struct {
	hub      *Hub
	tokens   TokenParser
	users    UserChecker
	chat     ChatService
	log      *slog.Logger
	upgrader websocket.Upgrader
	allowAll bool
	origins  map[string]struct{}
}

func NewHandler(hub *Hub, tokens TokenParser, users UserChecker, chat ChatService, origins []string, log *slog.Logger) *Handler {
	allowAll := false
	originSet := map[string]struct{}{}
	for _, o := range origins {
		o = strings.TrimRight(strings.TrimSpace(o), "/")
		if o == "*" {
			allowAll = true
			continue
		}
		originSet[o] = struct{}{}
	}
	h := &Handler{hub: hub, tokens: tokens, users: users, chat: chat, log: log, allowAll: allowAll, origins: originSet}
	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true // native mobile clients do not send Origin
			}
			if h.allowAll {
				return true
			}
			origin = strings.TrimRight(origin, "/")
			_, ok := h.origins[origin]
			return ok
		},
	}
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("token"))
	if raw == "" {
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			raw = strings.TrimSpace(auth[7:])
		}
	}
	if raw == "" {
		writeJSONError(w, http.StatusUnauthorized, "missing_token", "access token is required")
		return
	}
	claims, err := h.tokens.ParseAccess(raw)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "invalid_token", "access token is invalid or expired")
		return
	}
	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "invalid_token", "access token is invalid")
		return
	}
	ok, err := h.users.UserExists(r.Context(), userID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "could not verify user")
		return
	}
	if !ok {
		writeJSONError(w, http.StatusForbidden, "account_inactive", "account is not active")
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Warn("websocket upgrade failed", "error", err)
		return
	}
	client := &Client{
		hub:    h.hub,
		conn:   conn,
		chat:   h.chat,
		log:    h.log,
		userID: userID,
		send:   make(chan []byte, sendBuffer),
		done:   make(chan struct{}),
	}
	h.hub.Register(client)
	h.hub.SetOnline(context.Background(), userID)
	client.enqueueEvent("ready", map[string]any{
		"user_id":     userID,
		"server_time": time.Now().UTC(),
	})

	go client.writePump()
	client.readPump()
}

type Client struct {
	hub    *Hub
	conn   *websocket.Conn
	chat   ChatService
	log    *slog.Logger
	userID uuid.UUID
	send   chan []byte
	done   chan struct{}
	once   sync.Once
}

func (c *Client) closeSend() {
	c.once.Do(func() { close(c.done) })
}

func (c *Client) enqueue(payload []byte) {
	select {
	case <-c.done:
		return
	default:
	}
	select {
	case c.send <- payload:
	default:
		c.log.Warn("websocket send buffer overflow; closing client", "user_id", c.userID)
		c.conn.Close()
	}
}

func (c *Client) enqueueEvent(eventType string, data any) {
	ev := Event{Type: eventType, Data: data, TS: time.Now().UTC()}
	body, err := json.Marshal(ev)
	if err != nil {
		c.log.Error("marshal websocket event", "error", err)
		return
	}
	c.enqueue(body)
}

func (c *Client) enqueueError(requestID, code, message string) {
	data := map[string]any{"code": code, "message": message}
	if requestID != "" {
		data["request_id"] = requestID
	}
	c.enqueueEvent("error", data)
}

func (c *Client) readPump() {
	defer func() {
		c.hub.Unregister(c)
		c.hub.SetOffline(context.Background(), c.userID)
		c.closeSend()
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		c.hub.RefreshOnline(context.Background(), c.userID)
		return nil
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				c.log.Debug("websocket read closed", "user_id", c.userID, "error", err)
			}
			return
		}
		c.hub.RefreshOnline(context.Background(), c.userID)
		var msg inbound
		if err := json.Unmarshal(raw, &msg); err != nil {
			c.enqueueError("", "invalid_event", "event must be valid JSON")
			continue
		}
		c.handle(msg)
	}
}

type inbound struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

type messageSendData struct {
	ConversationID string  `json:"conversation_id"`
	Body           string  `json:"body"`
	MediaURL       *string `json:"media_url,omitempty"`
}

type conversationData struct {
	ConversationID string `json:"conversation_id"`
}

func (c *Client) handle(msg inbound) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch msg.Type {
	case "ping":
		c.enqueueEvent("pong", map[string]any{"server_time": time.Now().UTC()})
	case "message.send":
		var data messageSendData
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			c.enqueueError(msg.RequestID, "invalid_data", "invalid message.send payload")
			return
		}
		conversationID, err := uuid.Parse(data.ConversationID)
		if err != nil {
			c.enqueueError(msg.RequestID, "invalid_conversation", "conversation_id must be a UUID")
			return
		}
		m, err := c.chat.SendMessage(ctx, c.userID, conversationID, data.Body, data.MediaURL)
		if err != nil {
			c.enqueueError(msg.RequestID, errorCode(err), err.Error())
			return
		}
		c.enqueueEvent("message.ack", map[string]any{"request_id": msg.RequestID, "message": m})
	case "typing":
		var data conversationData
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			c.enqueueError(msg.RequestID, "invalid_data", "invalid typing payload")
			return
		}
		conversationID, err := uuid.Parse(data.ConversationID)
		if err != nil {
			c.enqueueError(msg.RequestID, "invalid_conversation", "conversation_id must be a UUID")
			return
		}
		if err := c.chat.Typing(ctx, c.userID, conversationID); err != nil {
			c.enqueueError(msg.RequestID, errorCode(err), err.Error())
		}
	case "message.read":
		var data conversationData
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			c.enqueueError(msg.RequestID, "invalid_data", "invalid message.read payload")
			return
		}
		conversationID, err := uuid.Parse(data.ConversationID)
		if err != nil {
			c.enqueueError(msg.RequestID, "invalid_conversation", "conversation_id must be a UUID")
			return
		}
		_, readAt, err := c.chat.MarkRead(ctx, c.userID, conversationID)
		if err != nil {
			c.enqueueError(msg.RequestID, errorCode(err), err.Error())
			return
		}
		c.enqueueEvent("message.read.ack", map[string]any{"request_id": msg.RequestID, "read_at": readAt})
	default:
		c.enqueueError(msg.RequestID, "unknown_event", "unsupported event type")
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case <-c.done:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		case payload, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return "not_found"
	case errors.Is(err, domain.ErrForbidden):
		return "forbidden"
	case errors.Is(err, domain.ErrConversationClosed):
		return "conversation_closed"
	case errors.Is(err, domain.ErrBlocked):
		return "blocked"
	case errors.Is(err, domain.ErrRateLimited):
		return "rate_limited"
	default:
		var v *domain.ValidationError
		if errors.As(err, &v) {
			return "validation_error"
		}
		return "bad_request"
	}
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"code": code, "message": message},
	})
}
