// Package ws implements the realtime gateway. Multiple API instances fan out
// events through Redis pub/sub; each instance only writes to its local sockets.
package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/cache"
)

const wsChannel = "love:ws:events"

type Event struct {
	Type string    `json:"type"`
	Data any       `json:"data,omitempty"`
	TS   time.Time `json:"ts"`
}

type fanout struct {
	Recipients []string `json:"recipients"`
	Event      Event    `json:"event"`
}

type Hub struct {
	mu      sync.RWMutex
	clients map[uuid.UUID]map[*Client]struct{}
	cache   *cache.Cache
	log     *slog.Logger
}

func NewHub(c *cache.Cache, log *slog.Logger) *Hub {
	return &Hub{
		clients: make(map[uuid.UUID]map[*Client]struct{}),
		cache:   c,
		log:     log,
	}
}

func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set, ok := h.clients[c.userID]
	if !ok {
		set = make(map[*Client]struct{})
		h.clients[c.userID] = set
	}
	set[c] = struct{}{}
	h.log.Info("ws client connected", "user_id", c.userID, "clients", h.countLocked(c.userID))
}

func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set, ok := h.clients[c.userID]
	if !ok {
		return
	}
	delete(set, c)
	if len(set) == 0 {
		delete(h.clients, c.userID)
	}
}

func (h *Hub) countLocked(userID uuid.UUID) int { return len(h.clients[userID]) }

func (h *Hub) OnlineUsers() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// SendToUser implements service.Broadcaster.
func (h *Hub) SendToUser(userID uuid.UUID, eventType string, data any) {
	h.SendToUsers([]uuid.UUID{userID}, eventType, data)
}

func (h *Hub) SendToUsers(userIDs []uuid.UUID, eventType string, data any) {
	if len(userIDs) == 0 {
		return
	}
	event := Event{Type: eventType, Data: data, TS: time.Now().UTC()}
	recipients := make([]string, 0, len(userIDs))
	seen := map[uuid.UUID]struct{}{}
	for _, id := range userIDs {
		if id == uuid.Nil {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		recipients = append(recipients, id.String())
	}
	if len(recipients) == 0 {
		return
	}
	payload, err := json.Marshal(fanout{Recipients: recipients, Event: event})
	if err != nil {
		h.log.Error("marshal websocket event", "error", err)
		return
	}
	if h.cache != nil && h.cache.Available() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := h.cache.Publish(ctx, wsChannel, payload)
		cancel()
		if err == nil {
			return
		}
		h.log.Warn("redis websocket publish failed; delivering locally", "error", err)
	}
	h.deliver(payload)
}

// DeliverPayload is used by the Redis subscriber.
func (h *Hub) DeliverPayload(payload []byte) { h.deliver(payload) }

func (h *Hub) deliver(payload []byte) {
	var msg fanout
	if err := json.Unmarshal(payload, &msg); err != nil {
		h.log.Warn("invalid websocket fanout payload", "error", err)
		return
	}
	body, err := json.Marshal(msg.Event)
	if err != nil {
		return
	}
	h.mu.RLock()
	targets := make([]*Client, 0)
	for _, raw := range msg.Recipients {
		id, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		for c := range h.clients[id] {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range targets {
		c.enqueue(body)
	}
}

func (h *Hub) SubscribeRedis(ctx context.Context) {
	if h.cache == nil || !h.cache.Available() {
		return
	}
	cancel, err := h.cache.Subscribe(ctx, wsChannel, h.DeliverPayload)
	if err != nil {
		h.log.Warn("redis websocket subscription failed", "error", err)
		return
	}
	go func() {
		<-ctx.Done()
		cancel()
	}()
}

func (h *Hub) SetOnline(ctx context.Context, userID uuid.UUID) {
	if h.cache != nil {
		h.cache.SetOnline(ctx, userID, 90*time.Second)
	}
}

func (h *Hub) RefreshOnline(ctx context.Context, userID uuid.UUID) {
	h.SetOnline(ctx, userID)
}

func (h *Hub) SetOffline(ctx context.Context, userID uuid.UUID) {
	if h.cache != nil {
		h.cache.SetOffline(ctx, userID)
	}
}
