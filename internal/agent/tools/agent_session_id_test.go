package tools

import (
	"sync"
	"testing"
)

// TestGenerateSessionID_NoCollisionsUnderConcurrency reproduces a real
// collision risk: generateSessionID used to be "child-" + time.Now().UnixNano(),
// pure wall-clock time with no randomness. instantiateChild calls it once per
// spawned child agent, and the coordinator explicitly supports spawning
// children concurrently (parallel tool calls), so two children minted on
// different goroutines within the same clock tick got the identical session
// ID. That ID becomes the child's persisted session row - sessions.Manager.Save
// upserts by ID and deletes+reinserts that ID's messages, so a collision
// silently merges one child's transcript into the other's instead of
// erroring. Runs many concurrent mints and asserts every ID is unique.
func TestGenerateSessionID_NoCollisionsUnderConcurrency(t *testing.T) {
	const n = 5000
	ids := make([]string, n)
	errs := make([]error, n)

	// A start barrier is essential to reproduce this: goroutines released one
	// at a time (bare `go func(){...}()` in a loop) pick up enough scheduling
	// jitter between spawn and body that the clock has almost always ticked
	// forward by the time each one reads it. Releasing all n at once via a
	// closed channel is what actually lines multiple goroutines up on the
	// same clock tick, matching how the coordinator fires off several
	// parallel "agent" tool calls at once.
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			<-start
			id, err := generateSessionID()
			ids[i] = id
			errs[i] = err
		}(i)
	}
	close(start)
	wg.Wait()

	seen := make(map[string]int, n)
	for i, id := range ids {
		if errs[i] != nil {
			t.Fatalf("generateSessionID returned error: %v", errs[i])
		}
		if id == "" {
			t.Fatalf("generateSessionID returned empty id at index %d", i)
		}
		if prev, ok := seen[id]; ok {
			t.Fatalf("generateSessionID produced a duplicate id %q at indexes %d and %d", id, prev, i)
		}
		seen[id] = i
	}
}
