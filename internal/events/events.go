// Package events provides the transactional-outbox publisher and a transport
// abstraction (Kafka in production, in-process local bus in tests/dev).
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"

	"github.com/loveservice/loveservice/internal/domain"
)

type Event struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Topic       string          `json:"topic"`
	AggregateID string          `json:"aggregate_id,omitempty"`
	Payload     json.RawMessage `json:"payload"`
	OccurredAt  time.Time       `json:"occurred_at"`
}

func New(eventType, aggregateID string, payload any) (Event, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return Event{}, err
	}
	return Event{
		ID:          uuid.NewString(),
		Type:        eventType,
		Topic:       "love.events",
		AggregateID: aggregateID,
		Payload:     body,
		OccurredAt:  time.Now().UTC(),
	}, nil
}

type Bus interface {
	Publish(ctx context.Context, event Event) error
	Close() error
}

type LocalBus struct {
	handler func(context.Context, Event) error
	log     *slog.Logger
}

func NewLocalBus(handler func(context.Context, Event) error, log *slog.Logger) *LocalBus {
	return &LocalBus{handler: handler, log: log}
}

func (b *LocalBus) Publish(ctx context.Context, event Event) error {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				b.log.Error("local event handler panicked", "event_type", event.Type, "panic", r)
			}
		}()
		hctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := b.handler(hctx, event); err != nil {
			b.log.Error("local event handler failed", "event_type", event.Type, "error", err)
		}
	}()
	return nil
}

func (b *LocalBus) Close() error { return nil }

type KafkaBus struct {
	writer *kafka.Writer
	log    *slog.Logger
}

func NewKafkaBus(brokers []string, topic string, log *slog.Logger) *KafkaBus {
	w := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafka.LeastBytes{},
		RequiredAcks: kafka.RequireAll,
		Async:        false,
		BatchTimeout: 20 * time.Millisecond,
		WriteTimeout: 10 * time.Second,
		ReadTimeout:  10 * time.Second,
	}
	return &KafkaBus{writer: w, log: log}
}

func (b *KafkaBus) Publish(ctx context.Context, event Event) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	key := event.AggregateID
	if key == "" {
		key = event.ID
	}
	return b.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(key),
		Value: body,
		Time:  event.OccurredAt,
		Headers: []kafka.Header{
			{Key: "event-type", Value: []byte(event.Type)},
			{Key: "event-id", Value: []byte(event.ID)},
		},
	})
}

func (b *KafkaBus) Close() error { return b.writer.Close() }

// Consumer reads the shared event stream with a consumer group and dispatches
// events to a handler. Handler errors are logged; offsets are committed by
// kafka-go after successful reads (at-least-once delivery).
type Consumer struct {
	reader  *kafka.Reader
	handler func(context.Context, Event) error
	log     *slog.Logger
}

func NewConsumer(brokers []string, topic, groupID string, handler func(context.Context, Event) error, log *slog.Logger) *Consumer {
	return &Consumer{
		reader: kafka.NewReader(kafka.ReaderConfig{
			Brokers:        brokers,
			Topic:          topic,
			GroupID:        groupID,
			MinBytes:       1,
			MaxBytes:       10 << 20,
			MaxWait:        500 * time.Millisecond,
			CommitInterval: time.Second,
			StartOffset:    kafka.FirstOffset,
		}),
		handler: handler,
		log:     log,
	}
}

func (c *Consumer) Run(ctx context.Context) {
	c.log.Info("kafka consumer started", "topic", c.reader.Config().Topic, "group", c.reader.Config().GroupID)
	for {
		msg, err := c.reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.log.Warn("kafka read failed", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			continue
		}
		var event Event
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			c.log.Error("invalid kafka event payload", "error", err, "offset", msg.Offset)
			continue
		}
		if err := c.handler(ctx, event); err != nil {
			c.log.Error("event handler failed", "event_type", event.Type, "event_id", event.ID, "error", err)
		}
	}
}

func (c *Consumer) Close() error { return c.reader.Close() }

// OutboxRepository is the subset of the store required by the publisher.
type OutboxRepository interface {
	FetchPendingOutbox(ctx context.Context, limit int) ([]domain.OutboxEvent, error)
	MarkOutboxPublished(ctx context.Context, id uuid.UUID) error
	MarkOutboxFailed(ctx context.Context, id uuid.UUID, reason string) error
}

type Publisher struct {
	repo     OutboxRepository
	bus      Bus
	log      *slog.Logger
	interval time.Duration
	lastLog  time.Time
}

func NewPublisher(repo OutboxRepository, bus Bus, log *slog.Logger) *Publisher {
	return &Publisher{repo: repo, bus: bus, log: log, interval: time.Second}
}

func (p *Publisher) Run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.flush(ctx)
		}
	}
}

func (p *Publisher) flush(ctx context.Context) {
	records, err := p.repo.FetchPendingOutbox(ctx, 100)
	if err != nil {
		p.log.Error("fetch outbox failed", "error", err)
		return
	}
	for _, rec := range records {
		event := Event{
			ID:          rec.ID.String(),
			Type:        rec.EventType,
			Topic:       rec.Topic,
			AggregateID: rec.AggregateID,
			Payload:     json.RawMessage(rec.Payload),
			OccurredAt:  rec.CreatedAt,
		}
		pubCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := p.bus.Publish(pubCtx, event)
		cancel()
		if err != nil {
			if time.Since(p.lastLog) > 10*time.Second {
				p.log.Warn("outbox publish failed; will retry", "event_type", rec.EventType, "error", err)
				p.lastLog = time.Now()
			}
			_ = p.repo.MarkOutboxFailed(ctx, rec.ID, err.Error())
			continue
		}
		if err := p.repo.MarkOutboxPublished(ctx, rec.ID); err != nil {
			p.log.Error("mark outbox published failed", "event_id", rec.ID, "error", err)
		}
	}
}

func (p *Publisher) String() string { return fmt.Sprintf("outbox-publisher(%s)", p.interval) }
