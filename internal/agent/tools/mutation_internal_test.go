package tools

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// lockCount reports the number of per-file entries currently in the queue's
// map. Internal test only: the map is unexported, and cleanup (the entry is
// deleted when its holder count reaches zero) is otherwise unobservable, so
// the leak check needs white-box access.
func lockCount(q *MutationQueue) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.locks)
}

func holdersOf(q *MutationQueue, path string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	entry := q.locks[path]
	if entry == nil {
		return 0
	}
	return entry.holders
}

// waitFor polls cond until it holds or the deadline passes; used to observe
// internal state that is only mutated inside Acquire/Release.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// After the last holder of a path releases, its map entry must be deleted -
// otherwise the map grows unboundedly across a long session.
func TestMutationQueue_PerEntryCleanupOnFinalRelease(t *testing.T) {
	q := NewMutationQueue()
	for i := range 8 {
		p := fmt.Sprintf("/tmp/file-%d.txt", i)
		release := q.Acquire(p)
		if got := lockCount(q); got != 1 {
			t.Fatalf("with one outstanding acquire the map holds %d entries, want 1", got)
		}
		release()
		if got := lockCount(q); got != 0 {
			t.Fatalf("entry for %s survived its final release (map size %d)", p, got)
		}
	}
}

// The entry must survive while holders remain: the second holder is blocked
// on the entry mutex but has already bumped the holder count, so releasing
// the first must not delete the entry.
func TestMutationQueue_EntrySurvivesUntilLastHolderReleases(t *testing.T) {
	q := NewMutationQueue()
	const p = "/tmp/shared.txt"

	r1 := q.Acquire(p)
	acquired := make(chan func(), 1)
	go func() {
		acquired <- q.Acquire(p) // blocks on entry.mu, but registers holders first
	}()
	waitFor(t, "second holder registration", func() bool { return holdersOf(q, p) == 2 })

	r1()
	if got := lockCount(q); got != 1 {
		t.Fatalf("entry deleted while a second holder was still pending (map size %d)", got)
	}

	r2 := <-acquired
	r2()
	if got := lockCount(q); got != 0 {
		t.Fatalf("entry not deleted after the final release (map size %d)", got)
	}
}

// Many paths mutated concurrently must all clean up after themselves.
func TestMutationQueue_ConcurrentCleanupLeavesNoEntries(t *testing.T) {
	q := NewMutationQueue()
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			p := fmt.Sprintf("/tmp/conc-%d.txt", i%8)
			for range 20 {
				release := q.Acquire(p)
				release()
			}
		})
	}
	wg.Wait()
	if got := lockCount(q); got != 0 {
		t.Fatalf("lock map retained %d entries after all releases", got)
	}
}

// A path whose entry was cleaned up must be re-acquirable and still
// serialize: exclusion must hold across entry generations.
func TestMutationQueue_ReacquireAfterCleanupStillSerializes(t *testing.T) {
	q := NewMutationQueue()
	const p = "/tmp/reacquire.txt"

	for range 10 {
		release := q.Acquire(p)
		release()
	}
	if got := lockCount(q); got != 0 {
		t.Fatalf("expected empty map before re-acquire stress, got %d", got)
	}

	var counter atomic.Int64
	var maxConcurrent atomic.Int64
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			release := q.Acquire(p)
			defer release()
			cur := counter.Add(1)
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}
			counter.Add(-1)
		})
	}
	wg.Wait()

	if maxConcurrent.Load() > 1 {
		t.Fatalf("expected max concurrency of 1 for one path, got %d", maxConcurrent.Load())
	}
	if got := lockCount(q); got != 0 {
		t.Fatalf("lock map retained %d entries after re-acquire stress", got)
	}
}
