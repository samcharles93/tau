package tools_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
)

// waitGroupWithTimeout fails the test instead of hanging forever when a
// lock regression deadlocks the queue.
func waitGroupWithTimeout(t *testing.T, wg *sync.WaitGroup, d time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("timed out after %s waiting for goroutines (possible MutationQueue deadlock)", d)
	}
}

func TestMutationQueue_Serializes(t *testing.T) {
	q := tools.NewMutationQueue()
	path := "/tmp/test.txt"

	var counter atomic.Int64
	var maxConcurrent atomic.Int64
	var wg sync.WaitGroup

	for i := range 50 {
		_ = i
		wg.Go(func() {
			release := q.Acquire(path)
			defer release()

			cur := counter.Add(1)
			// Track max concurrency for this path.
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
		t.Fatalf("expected max concurrency of 1, got %d", maxConcurrent.Load())
	}
}

func TestMutationQueue_DifferentPaths_Parallel(t *testing.T) {
	q := tools.NewMutationQueue()

	var wg sync.WaitGroup
	started := make(chan struct{}, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		release := q.Acquire("/path/a")
		started <- struct{}{}
		<-started // wait for both to acquire
		release()
	}()
	go func() {
		defer wg.Done()
		release := q.Acquire("/path/b")
		started <- struct{}{}
		<-started // wait for both to acquire
		release()
	}()

	wg.Wait()
	// If we get here without deadlocking, different paths don't block each other.
}

// GlobalLock (the shell tool's global write lock) must block new per-file
// Acquire calls until GlobalUnlock.
func TestMutationQueue_GlobalLockBlocksNewAcquire(t *testing.T) {
	q := tools.NewMutationQueue()
	q.GlobalLock()

	acquired := make(chan func(), 1)
	go func() {
		acquired <- q.Acquire("/blocked.txt")
	}()

	select {
	case <-acquired:
		t.Fatal("Acquire proceeded while the global lock was held")
	case <-time.After(100 * time.Millisecond):
	}

	q.GlobalUnlock()

	select {
	case release := <-acquired:
		release()
	case <-time.After(5 * time.Second):
		t.Fatal("Acquire did not proceed after GlobalUnlock")
	}
}

// GlobalLock must wait for in-flight per-file holders to release before it
// acquires (RWMutex writer semantics).
func TestMutationQueue_GlobalLockWaitsForInFlightHolders(t *testing.T) {
	q := tools.NewMutationQueue()
	release := q.Acquire("/inflight.txt")

	locked := make(chan struct{})
	go func() {
		q.GlobalLock()
		close(locked)
	}()

	select {
	case <-locked:
		t.Fatal("GlobalLock acquired while a per-file lock was still held")
	case <-time.After(100 * time.Millisecond):
	}

	release()

	select {
	case <-locked:
	case <-time.After(5 * time.Second):
		t.Fatal("GlobalLock did not acquire after the in-flight holder released")
	}
	q.GlobalUnlock()
}

// A held per-file lock must not be able to proceed past a pending GlobalLock
// in a way that deadlocks: the global writer waits for the holder, the
// holder never needs the global lock, so the pair always resolves.
func TestMutationQueue_HeldPerFileLockAndGlobalLockDoNotDeadlock(t *testing.T) {
	q := tools.NewMutationQueue()

	var wg sync.WaitGroup
	wg.Go(func() {
		release := q.Acquire("/contended.txt")
		// Hold the per-file lock while a GlobalLock is pending.
		time.Sleep(50 * time.Millisecond)
		release()
	})
	wg.Go(func() {
		time.Sleep(10 * time.Millisecond) // ensure the per-file lock is taken first
		q.GlobalLock()
		q.GlobalUnlock()
	})

	waitGroupWithTimeout(t, &wg, 5*time.Second)
}

// Stress the RWMutex ordering under the race detector: mutators hammer
// per-file Acquire/Release while a "shell" goroutine repeatedly takes the
// global lock. No mutation may be inside its per-file critical section while
// the global lock is held.
func TestMutationQueue_GlobalLockSerializesAgainstMutations(t *testing.T) {
	q := tools.NewMutationQueue()
	paths := []string{"/a.txt", "/b.txt", "/c.txt"}

	var inFlight atomic.Int64
	var shellHeld atomic.Bool
	var overlap atomic.Bool

	stop := make(chan struct{})
	var wg sync.WaitGroup

	for i := range 8 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				release := q.Acquire(paths[i%len(paths)])
				inFlight.Add(1)
				if shellHeld.Load() {
					overlap.Store(true)
				}
				inFlight.Add(-1)
				release()
			}
		})
	}
	wg.Go(func() {
		for range 50 {
			q.GlobalLock()
			shellHeld.Store(true)
			time.Sleep(time.Millisecond) // widen the window so mutators can observe the hold
			shellHeld.Store(false)
			q.GlobalUnlock()
		}
		close(stop)
	})

	waitGroupWithTimeout(t, &wg, 30*time.Second)

	if overlap.Load() {
		t.Fatal("a file mutation ran inside its critical section while the global (shell) lock was held")
	}
	if got := inFlight.Load(); got != 0 {
		t.Fatalf("in-flight mutation count is %d after all goroutines finished, want 0", got)
	}
}
