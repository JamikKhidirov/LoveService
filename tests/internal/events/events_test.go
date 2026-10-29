// Package events_test contains black-box tests for internal/events.
package events_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/domain"
	"github.com/loveservice/loveservice/internal/events"
)

func TestNewEventEncodesPayload(t *testing.T) {
	ev, err := events.New("match.created", "agg-1", map[string]string{"match_id": "m1"})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "match.created" || ev.AggregateID != "agg-1" || ev.ID == "" {
		t.Fatalf("unexpected event %+v", ev)
	}
	var payload map[string]string
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["match_id"] != "m1" {
		t.Fatalf("payload = %v", payload)
	}
}

func TestLocalBusDeliversAsynchronously(t *testing.T) {
	got := make(chan events.Event, 1)
	bus := events.NewLocalBus(func(_ context.Context, ev events.Event) error {
		got <- ev
		return nil
	}, slog.Default())
	defer bus.Close()

	ev, _ := events.New("test.event", "", map[string]int{"n": 1})
	if err := bus.Publish(context.Background(), ev); err != nil {
		t.Fatal(err)
	}

	select {
	case delivered := <-got:
		if delivered.Type != "test.event" {
			t.Fatalf("delivered %q", delivered.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event was not delivered")
	}
}

func TestLocalBusSurvivesHandlerErrorAndPanic(t *testing.T) {
	done := make(chan struct{}, 2)
	bus := events.NewLocalBus(func(_ context.Context, _ events.Event) error {
		defer func() { done <- struct{}{} }()
		if len(done) == 0 {
			panic("boom")
		}
		return errors.New("handler failed")
	}, slog.Default())
	defer bus.Close()

	ev, _ := events.New("test.event", "", map[string]int{"n": 1})
	if err := bus.Publish(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not run")
	}
}

type fakeOutbox struct {
	pending   []domain.OutboxEvent
	published []uuid.UUID
	failed    map[uuid.UUID]string
}

func (f *fakeOutbox) FetchPendingOutbox(_ context.Context, _ int) ([]domain.OutboxEvent, error) {
	rows := f.pending
	f.pending = nil
	return rows, nil
}

func (f *fakeOutbox) MarkOutboxPublished(_ context.Context, id uuid.UUID) error {
	f.published = append(f.published, id)
	return nil
}

func (f *fakeOutbox) MarkOutboxFailed(_ context.Context, id uuid.UUID, reason string) error {
	if f.failed == nil {
		f.failed = map[uuid.UUID]string{}
	}
	f.failed[id] = reason
	return nil
}

type fakeBus struct {
	got  chan events.Event
	fail bool
}

func (b *fakeBus) Publish(_ context.Context, ev events.Event) error {
	if b.fail {
		return errors.New("broker down")
	}
	b.got <- ev
	return nil
}

func (b *fakeBus) Close() error { return nil }

func TestPublisherPublishesAndMarks(t *testing.T) {
	repo := &fakeOutbox{pending: []domain.OutboxEvent{{
		ID: uuid.New(), Topic: "love.events", EventType: "match.created",
		AggregateID: "agg", Payload: []byte(`{"ok":true}`), CreatedAt: time.Now(),
	}}}
	bus := &fakeBus{got: make(chan events.Event, 1)}
	pub := events.NewPublisher(repo, bus, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pub.Run(ctx)

	select {
	case ev := <-bus.got:
		if ev.Type != "match.created" {
			t.Fatalf("published %q", ev.Type)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("publisher did not publish")
	}

	deadline := time.After(3 * time.Second)
	for len(repo.published) == 0 {
		select {
		case <-deadline:
			t.Fatal("outbox row was not marked published")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestPublisherMarksFailed(t *testing.T) {
	id := uuid.New()
	repo := &fakeOutbox{pending: []domain.OutboxEvent{{
		ID: id, Topic: "love.events", EventType: "match.created",
		Payload: []byte(`{}`), CreatedAt: time.Now(),
	}}}
	pub := events.NewPublisher(repo, &fakeBus{fail: true}, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pub.Run(ctx)

	deadline := time.After(3 * time.Second)
	for {
		if reason, ok := repo.failed[id]; ok {
			if reason == "" {
				t.Fatal("expected failure reason")
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("outbox row was not marked failed")
		case <-time.After(20 * time.Millisecond):
		}
	}
}
