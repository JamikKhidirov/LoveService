// Package cache wraps Redis and provides graceful in-process fallbacks for
// local development and tests. Redis is never required for correctness.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const wsChannel = "love:ws:events"

type Cache struct {
	rdb  *redis.Client
	log  *slog.Logger
	mu   sync.Mutex
	mem  map[string]memEntry
	stop chan struct{}
}

type memEntry struct {
	value   int64
	data    []byte
	hasData bool
	expires time.Time
}

// setMem stores raw bytes with a TTL. It is used by presence and JSON caching
// when Redis is not available.
func (c *Cache) setMem(key string, data []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mem[key] = memEntry{data: data, hasData: true, expires: time.Now().Add(ttl)}
}

// getMem returns unexpired bytes.
func (c *Cache) getMem(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.mem[key]
	if !ok {
		return nil, false
	}
	if e.expires.Before(time.Now()) {
		delete(c.mem, key)
		return nil, false
	}
	if !e.hasData {
		return nil, false
	}
	return e.data, true
}

func (c *Cache) deleteMem(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.mem, key)
}

func New(ctx context.Context, addr, password string, db int, log *slog.Logger) *Cache {
	c := &Cache{log: log, mem: make(map[string]memEntry), stop: make(chan struct{})}
	if addr == "" {
		log.Warn("redis address is empty; using in-memory rate limiter and local websocket fanout")
		return c
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		log.Warn("redis is unavailable; using degraded in-memory mode", "error", err)
		_ = rdb.Close()
		return c
	}
	c.rdb = rdb
	return c
}

func (c *Cache) Available() bool { return c != nil && c.rdb != nil }

func (c *Cache) Close() error {
	if c == nil {
		return nil
	}
	select {
	case <-c.stop:
	default:
		close(c.stop)
	}
	if c.rdb != nil {
		return c.rdb.Close()
	}
	return nil
}

// Incr increments a counter with a TTL and returns its new value. When Redis is
// unavailable an in-memory counter is used (single-instance only).
func (c *Cache) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if c == nil {
		return 0, fmt.Errorf("cache is not configured")
	}
	if c.rdb != nil {
		pipe := c.rdb.TxPipeline()
		incr := pipe.Incr(ctx, key)
		pipe.Expire(ctx, key, ttl)
		if _, err := pipe.Exec(ctx); err != nil {
			return 0, err
		}
		return incr.Val(), nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if e, ok := c.mem[key]; ok && e.expires.After(now) {
		e.value++
		c.mem[key] = e
		return e.value, nil
	}
	c.mem[key] = memEntry{value: 1, expires: now.Add(ttl)}
	return 1, nil
}

func (c *Cache) Count(ctx context.Context, key string) int64 {
	if c == nil {
		return 0
	}
	if c.rdb != nil {
		v, err := c.rdb.Get(ctx, key).Int64()
		if err != nil {
			return 0
		}
		return v
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.mem[key]
	if !ok || e.expires.Before(time.Now()) {
		return 0
	}
	return e.value
}

// Allow implements a fixed-window rate limiter.
func (c *Cache) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, int, error) {
	if limit <= 0 {
		return true, limit, nil
	}
	count, err := c.Incr(ctx, "rl:"+key, window)
	if err != nil {
		return true, 0, err
	}
	return count <= int64(limit), int(int64(limit) - count), nil
}

func (c *Cache) SetOnline(ctx context.Context, userID uuid.UUID, ttl time.Duration) {
	if c == nil {
		return
	}
	key := "presence:" + userID.String()
	if !c.Available() {
		c.setMem(key, []byte("1"), ttl)
		return
	}
	_ = c.rdb.Set(ctx, key, "1", ttl).Err()
}

func (c *Cache) SetOffline(ctx context.Context, userID uuid.UUID) {
	if c == nil {
		return
	}
	key := "presence:" + userID.String()
	if !c.Available() {
		c.deleteMem(key)
		return
	}
	_ = c.rdb.Del(ctx, key).Err()
}

func (c *Cache) IsOnline(ctx context.Context, userID uuid.UUID) bool {
	if c == nil {
		return false
	}
	key := "presence:" + userID.String()
	if !c.Available() {
		_, ok := c.getMem(key)
		return ok
	}
	n, err := c.rdb.Exists(ctx, key).Result()
	return err == nil && n > 0
}

func (c *Cache) OnlineMany(ctx context.Context, ids []uuid.UUID) map[uuid.UUID]bool {
	out := make(map[uuid.UUID]bool, len(ids))
	if c == nil || len(ids) == 0 {
		return out
	}
	if !c.Available() {
		for _, id := range ids {
			out[id] = c.IsOnline(ctx, id)
		}
		return out
	}
	pipe := c.rdb.Pipeline()
	cmds := make(map[uuid.UUID]*redis.IntCmd, len(ids))
	for _, id := range ids {
		cmds[id] = pipe.Exists(ctx, "presence:"+id.String())
	}
	_, _ = pipe.Exec(ctx)
	for id, cmd := range cmds {
		out[id] = cmd.Val() > 0
	}
	return out
}

// Publish sends a JSON payload to every websocket instance.
func (c *Cache) Publish(ctx context.Context, channel string, payload []byte) error {
	if !c.Available() {
		return fmt.Errorf("redis is not available")
	}
	if channel == "" {
		channel = wsChannel
	}
	return c.rdb.Publish(ctx, channel, payload).Err()
}

// Subscribe listens for cross-instance websocket events. It returns a cancel
// function. The callback must be safe for concurrent use.
func (c *Cache) Subscribe(ctx context.Context, channel string, callback func([]byte)) (func(), error) {
	if !c.Available() {
		return func() {}, fmt.Errorf("redis is not available")
	}
	if channel == "" {
		channel = wsChannel
	}
	sub := c.rdb.Subscribe(ctx, channel)
	if _, err := sub.Receive(ctx); err != nil {
		return func() {}, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.stop:
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				callback([]byte(msg.Payload))
			}
		}
	}()
	return func() {
		_ = sub.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}, nil
}

func (c *Cache) SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error {
	if c == nil {
		return fmt.Errorf("cache is not configured")
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if !c.Available() {
		c.setMem(key, b, ttl)
		return nil
	}
	return c.rdb.Set(ctx, key, b, ttl).Err()
}

func (c *Cache) GetJSON(ctx context.Context, key string, dst any) (bool, error) {
	if c == nil {
		return false, fmt.Errorf("cache is not configured")
	}
	if !c.Available() {
		b, ok := c.getMem(key)
		if !ok {
			return false, nil
		}
		return true, json.Unmarshal(b, dst)
	}
	b, err := c.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(b, dst)
}

func DailyKey(prefix string, userID uuid.UUID, t time.Time) string {
	return prefix + ":" + userID.String() + ":" + t.UTC().Format("20060102")
}

func SecondsUntilMidnight(t time.Time) time.Duration {
	t = t.UTC()
	next := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).Add(24 * time.Hour)
	return next.Sub(t)
}

func LimitKey(kind string, userID uuid.UUID) string {
	return "limit:" + kind + ":" + userID.String()
}

func Int64String(v int64) string { return strconv.FormatInt(v, 10) }
