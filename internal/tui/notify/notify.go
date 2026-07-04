// Package notify provides a queue-based notification system for the TUI.
// Notifications are short-lived messages shown in the status bar that
// auto-dismiss after a configurable duration. Any part of the application
// can publish notifications via the pubsub bus; the TUI subscribes and
// enqueues them for display.
package notify

import "time"

// Level controls how a notification is rendered.
type Level int

const (
	LevelInfo Level = iota
	LevelWarn
	LevelError
)

// Notification is a transient message shown in the status bar.
type Notification struct {
	Message  string
	Level    Level
	Duration time.Duration
}

// entry is an enqueued notification with its computed expiry.
type entry struct {
	notification Notification
	expiresAt    time.Time
}

// Queue manages a FIFO queue of active notifications.
// Only the front notification is displayed; expired entries are discarded.
type Queue struct {
	items []entry
}

// NewQueue creates an empty notification queue.
func NewQueue() *Queue {
	return &Queue{}
}

// Push adds a notification to the back of the queue. A non-positive Duration
// means the notification never expires on its own — it stays current until
// Dismiss is called or it's overtaken by earlier entries expiring.
func (q *Queue) Push(n Notification) {
	var expiresAt time.Time
	if n.Duration > 0 {
		expiresAt = time.Now().Add(n.Duration)
	}
	q.items = append(q.items, entry{
		notification: n,
		expiresAt:    expiresAt,
	})
}

// Current returns the front (active) notification, or nil if the queue
// is empty after pruning expired entries.
func (q *Queue) Current() *Notification {
	q.prune()
	if len(q.items) == 0 {
		return nil
	}
	return &q.items[0].notification
}

// ExpiresAt returns when the front notification expires.
// Returns zero time if the queue is empty.
func (q *Queue) ExpiresAt() time.Time {
	q.prune()
	if len(q.items) == 0 {
		return time.Time{}
	}
	return q.items[0].expiresAt
}

// Dismiss removes the front notification immediately.
func (q *Queue) Dismiss() {
	q.prune()
	if len(q.items) > 0 {
		q.items = q.items[1:]
	}
}

// IsEmpty returns true if no active notifications remain.
func (q *Queue) IsEmpty() bool {
	q.prune()
	return len(q.items) == 0
}

// prune removes all expired entries from the front. A zero expiresAt means
// the entry never expires on its own (see Push) and is left in place.
func (q *Queue) prune() {
	now := time.Now()
	for len(q.items) > 0 {
		exp := q.items[0].expiresAt
		if exp.IsZero() || !now.After(exp) {
			break
		}
		q.items[0] = entry{} // zero out to allow GC of notification data
		q.items = q.items[1:]
	}
	// Reclaim backing array when fully drained to prevent unbounded growth.
	if len(q.items) == 0 {
		q.items = nil
	}
}
