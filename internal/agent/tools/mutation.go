package tools

import (
	"sync"
)

// MutationQueue serializes write operations to the same file path,
// preventing concurrent edits from clobbering each other during
// parallel tool execution.
type MutationQueue struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewMutationQueue creates a new per-file mutation queue.
func NewMutationQueue() *MutationQueue {
	return &MutationQueue{
		locks: make(map[string]*sync.Mutex),
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
	fileLock, ok := q.locks[path]
	if !ok {
		fileLock = &sync.Mutex{}
		q.locks[path] = fileLock
	}
	q.mu.Unlock()

	fileLock.Lock()
	return fileLock.Unlock
}
