package eventbus

import (
	"testing"
	"time"
)

type testEvent struct{ Msg string }

func TestBusPublishSubscribe(t *testing.T) {
	bus := New()
	defer bus.Close()

	pubClient := bus.Client("publisher")
	subClient := bus.Client("subscriber")

	pub := Publish[testEvent](pubClient)
	sub := Subscribe[testEvent](subClient)

	pub.Publish(testEvent{Msg: "hello"})

	select {
	case ev := <-sub.Events():
		if ev.Msg != "hello" {
			t.Errorf("got %q, want hello", ev.Msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestBusSameClientPublishSubscribe(t *testing.T) {
	bus := New()
	defer bus.Close()

	client := bus.Client("both")
	pub := Publish[testEvent](client)
	sub := Subscribe[testEvent](client)

	pub.Publish(testEvent{Msg: "same-client"})

	select {
	case ev := <-sub.Events():
		if ev.Msg != "same-client" {
			t.Errorf("got %q, want same-client", ev.Msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestBusMultipleSubscribers(t *testing.T) {
	bus := New()
	defer bus.Close()

	pubClient := bus.Client("publisher")
	subClient1 := bus.Client("subscriber1")
	subClient2 := bus.Client("subscriber2")

	pub := Publish[testEvent](pubClient)
	sub1 := Subscribe[testEvent](subClient1)
	sub2 := Subscribe[testEvent](subClient2)

	pub.Publish(testEvent{Msg: "broadcast"})

	for i, sub := range []*Subscriber[testEvent]{sub1, sub2} {
		select {
		case ev := <-sub.Events():
			if ev.Msg != "broadcast" {
				t.Errorf("subscriber %d: got %q, want broadcast", i, ev.Msg)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out", i)
		}
	}
}

func TestBusShouldPublish(t *testing.T) {
	bus := New()
	defer bus.Close()

	client := bus.Client("test")
	pub := Publish[testEvent](client)

	if pub.ShouldPublish() {
		t.Error("ShouldPublish should be false with no subscribers")
	}

	sub := Subscribe[testEvent](bus.Client("sub"))
	defer sub.Close()

	if !pub.ShouldPublish() {
		t.Error("ShouldPublish should be true with a subscriber")
	}
}

func TestBusCloseStopsDelivery(t *testing.T) {
	bus := New()

	client := bus.Client("test")
	pub := Publish[testEvent](client)
	sub := Subscribe[testEvent](client)

	pub.Publish(testEvent{Msg: "before-close"})

	select {
	case <-sub.Events():
		// ok
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pre-close event")
	}

	bus.Close()

	// Publish after close should silently do nothing.
	pub.Publish(testEvent{Msg: "after-close"})

	select {
	case <-sub.Events():
		t.Error("received event after bus close")
	case <-time.After(100 * time.Millisecond):
		// expected — no event delivered
	}

	// Events channel should eventually close... actually it won't
	// because bus.Close() closes clients which closes subscribers
	// which stops the dispatch goroutines. But the channel itself
	// is not closed (by design — reading from a closed channel
	// panics, and the typed send would panic too).
}
