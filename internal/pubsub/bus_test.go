package pubsub

import (
	"context"
	"errors"
	"testing"
)

func TestBusPublishAndSubscribe(t *testing.T) {
	bus := New[string]()
	subscription, err := bus.Subscribe("events", 1)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	if err := bus.Publish(context.Background(), "events", "hello"); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	message, ok := <-subscription.Channel()
	if !ok {
		t.Fatal("subscription channel closed unexpectedly")
	}
	if message != "hello" {
		t.Fatalf("message = %q, want %q", message, "hello")
	}
}

func TestSubscriptionUnsubscribeClosesChannel(t *testing.T) {
	bus := New[string]()
	subscription, err := bus.Subscribe("events", 0)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	subscription.Unsubscribe()

	_, ok := <-subscription.Channel()
	if ok {
		t.Fatal("subscription channel should be closed after unsubscribe")
	}

	if err := bus.Publish(context.Background(), "events", "ignored"); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
}

func TestBusCloseStopsOperations(t *testing.T) {
	bus := New[string]()
	subscription, err := bus.Subscribe("events", 1)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	bus.Close()

	_, ok := <-subscription.Channel()
	if ok {
		t.Fatal("subscription channel should be closed after Close")
	}

	if _, err := bus.Subscribe("events", 1); !errors.Is(err, ErrClosed) {
		t.Fatalf("Subscribe() error = %v, want %v", err, ErrClosed)
	}
	if err := bus.Publish(context.Background(), "events", "hello"); !errors.Is(err, ErrClosed) {
		t.Fatalf("Publish() error = %v, want %v", err, ErrClosed)
	}
}

func TestBusPublishHonorsContextCancellation(t *testing.T) {
	bus := New[string]()
	_, err := bus.Subscribe("events", 0)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := bus.Publish(ctx, "events", "hello"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Publish() error = %v, want %v", err, context.Canceled)
	}
}
