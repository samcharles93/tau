package notify

import (
	"testing"
	"time"
)

// TestQueue_ZeroDurationPersists guards against a regression where
// Duration: 0 (documented as "never expires") was computed as
// expiresAt = time.Now(), so prune() discarded it on the very next check -
// a "persistent" notification vanished almost instantly instead of staying
// current until Dismiss.
func TestQueue_ZeroDurationPersists(t *testing.T) {
	q := NewQueue()
	q.Push(Notification{Message: "persistent error", Level: LevelError, Duration: 0})

	time.Sleep(5 * time.Millisecond)

	n := q.Current()
	if n == nil {
		t.Fatal("expected zero-duration notification to still be current")
	}
	if n.Message != "persistent error" {
		t.Fatalf("unexpected message: %q", n.Message)
	}
}

// TestQueue_PositiveDurationExpires confirms a normal, positive-duration
// notification still expires once its window elapses.
func TestQueue_PositiveDurationExpires(t *testing.T) {
	q := NewQueue()
	q.Push(Notification{Message: "transient", Duration: 10 * time.Millisecond})

	if q.Current() == nil {
		t.Fatal("expected notification to be current immediately after push")
	}

	time.Sleep(20 * time.Millisecond)

	if q.Current() != nil {
		t.Fatal("expected notification to have expired")
	}
}

// TestQueue_FIFOOrderWithMixedDurations verifies a persistent (zero-duration)
// entry queued behind an already-expired transient entry is still reached
// once pruning advances past the expired one.
func TestQueue_FIFOOrderWithMixedDurations(t *testing.T) {
	q := NewQueue()
	q.Push(Notification{Message: "first", Duration: 5 * time.Millisecond})
	q.Push(Notification{Message: "second", Duration: 0})

	time.Sleep(10 * time.Millisecond)

	n := q.Current()
	if n == nil {
		t.Fatal("expected the persistent second entry to be current")
	}
	if n.Message != "second" {
		t.Fatalf("expected FIFO order to surface %q, got %q", "second", n.Message)
	}
}

// TestQueue_DismissRemovesFrontOnly verifies Dismiss removes exactly the
// front entry and exposes the next one, if any.
func TestQueue_DismissRemovesFrontOnly(t *testing.T) {
	q := NewQueue()
	q.Push(Notification{Message: "first", Duration: 0})
	q.Push(Notification{Message: "second", Duration: 0})

	q.Dismiss()

	n := q.Current()
	if n == nil {
		t.Fatal("expected a notification to remain after dismissing the front one")
	}
	if n.Message != "second" {
		t.Fatalf("expected %q after dismiss, got %q", "second", n.Message)
	}

	q.Dismiss()
	if !q.IsEmpty() {
		t.Fatal("expected queue to be empty after dismissing both entries")
	}
}
