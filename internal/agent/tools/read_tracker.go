package tools

import (
	"fmt"
	"path/filepath"
	"sync"
)

// ReadTracker records which files the model has read so that mutation
// tools (write, edit) can enforce a read-before-write safety
// check.
type ReadTracker struct {
	mu    sync.Mutex
	reads map[string]bool // absolute paths that have been read
}

// NewReadTracker creates a new ReadTracker.
func NewReadTracker() *ReadTracker {
	return &ReadTracker{
		reads: make(map[string]bool),
	}
}

// MarkRead records that a file at the given path has been read by the
// model. The path is normalised to absolute form before recording.
func (rt *ReadTracker) MarkRead(cwd, path string) {
	abs := resolvePath(cwd, path)
	rt.mu.Lock()
	rt.reads[abs] = true
	rt.mu.Unlock()
}

// CheckRead returns an error if the file at the given path has not been
// read by the model in this session. The path is normalised to absolute
// form. A file must be read (via the read tool) before it can be written,
// edited.
func (rt *ReadTracker) CheckRead(cwd, path string) error {
	abs := resolvePath(cwd, path)
	rt.mu.Lock()
	read := rt.reads[abs]
	rt.mu.Unlock()

	if !read {
		rel, _ := filepath.Rel(cwd, abs)
		if rel == "" {
			rel = abs
		}
		return fmt.Errorf(
			"file %q has not been read in this session - use the read tool first to read the file before writing or editing it",
			rel,
		)
	}
	return nil
}
