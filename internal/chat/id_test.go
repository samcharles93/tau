package chat

import (
	"strings"
	"sync"
	"testing"
)

func TestNewID_ReturnsDistinctUUIDv7(t *testing.T) {
	a, err := NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	b, err := NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	if a == b {
		t.Fatalf("two calls to NewID returned the same id %q", a)
	}
	if !strings.Contains(a, "-") {
		t.Fatalf("expected a UUID-shaped id, got %q", a)
	}
}

// TestNewID_NoCollisionsUnderConcurrency guards the property that motivated
// consolidating every ID helper onto NewID: internal/agent/tools once minted
// child agent session IDs from a bare time.Now().UnixNano(), which collided
// when several children were spawned concurrently (see generateSessionID's
// history). A start barrier releases all goroutines at once so they call
// NewID on the same clock tick, matching how the coordinator actually fires
// off several parallel tool calls - a bare `go func(){...}()` loop without
// the barrier doesn't reproduce this; goroutine-spawn jitter alone is enough
// for the clock to advance between calls.
func TestNewID_NoCollisionsUnderConcurrency(t *testing.T) {
	const n = 5000
	ids := make([]string, n)

	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			<-start
			id, err := NewID()
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = id
		}(i)
	}
	close(start)
	wg.Wait()

	seen := make(map[string]int, n)
	for i, id := range ids {
		if prev, ok := seen[id]; ok {
			t.Fatalf("NewID produced a duplicate id %q at indexes %d and %d", id, prev, i)
		}
		seen[id] = i
	}
}

func TestNewMessageID_ReturnsDistinctIDs(t *testing.T) {
	a := NewMessageID()
	b := NewMessageID()
	if a == "" || b == "" {
		t.Fatal("expected non-empty message ids")
	}
	if a == b {
		t.Fatalf("two calls to NewMessageID returned the same id %q", a)
	}
}

func TestNewRequestID_ReturnsDistinctIDs(t *testing.T) {
	a := NewRequestID()
	b := NewRequestID()
	if a == "" || b == "" {
		t.Fatal("expected non-empty request ids")
	}
	if a == b {
		t.Fatalf("two calls to NewRequestID returned the same id %q", a)
	}
}
