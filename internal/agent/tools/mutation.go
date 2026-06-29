package tools

import (
	"sync"
)

// MutationQueue serializes write operations to the same file path,
// preventing concurrent edits from clobbering each other during
// parallel tool execution.
type MutationQueue struct {
	mu    sync.Mutex
	locks map[string]*mutexEntry
}

// mutexEntry tracks a per-file mutex and its active holder count.
type mutexEntry struct {
	mu      sync.Mutex
	holders int
}

// NewMutationQueue creates a new per-file mutation queue.
func NewMutationQueue() *MutationQueue {
	return &MutationQueue{
		locks: make(map[string]*mutexEntry),
	}
}

// Acquire returns a lock for the given file path. The caller must call
// the returned release function when done with the mutation.
//
// Usage:
//
//	release := q.Acquire("/path/to/file.go")
//	defer release()
//	// ... perform read-modify-write ...
func (q *MutationQueue) Acquire(path string) (release func()) {
	q.mu.Lock()
	entry, ok := q.locks[path]
	if !ok {
		entry = &mutexEntry{}
		q.locks[path] = entry
	}
	entry.holders++
	q.mu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()

		q.mu.Lock()
		entry.holders--
		if entry.holders == 0 {
			delete(q.locks, path)
		}
		q.mu.Unlock()
	}
}
