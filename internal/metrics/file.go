// Package metrics provides subscribers for the chat.MetricEvent stream:
// a file-based JSONL exporter and a per-session usage tracker.
package metrics

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
)

// FileSubscriber appends MetricEvents as JSONL to a file. It acquires a
// per-event mutex to serialize writes, since the bus delivers events
// sequentially on a single goroutine for the subscribing client.
type FileSubscriber struct {
	sub    *eventbus.SubscriberFunc[chat.MetricEvent]
	f      *os.File
	mu     sync.Mutex
	closed bool
}

// NewFileSubscriber creates a subscriber that writes metrics to the given
// directory as a single metrics.jsonl file. The directory is created if
// it doesn't exist. Returns an error if the directory cannot be created
// or the file cannot be opened for appending.
func NewFileSubscriber(client *eventbus.Client, dir string) (*FileSubscriber, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("metrics dir: %w", err)
	}

	path := filepath.Join(dir, "metrics.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("metrics file: %w", err)
	}

	fs := &FileSubscriber{f: f}
	fs.sub = eventbus.SubscribeFunc(client, fs.handle)
	return fs, nil
}

func (fs *FileSubscriber) handle(e chat.MetricEvent) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	b, err := json.Marshal(e)
	if err != nil {
		slog.Warn("metrics file subscriber: json marshal failed", "err", err)
		return
	}
	b = append(b, '\n')
	if _, err := fs.f.Write(b); err != nil {
		slog.Warn("metrics file subscriber: write failed", "err", err)
	}
}

// Close closes the subscriber and the underlying file. The file is
// synced to disk before closing to ensure buffered writes are durable.
// Close is safe to call multiple times.
func (fs *FileSubscriber) Close() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if fs.closed {
		return nil
	}
	fs.closed = true

	fs.sub.Close()
	// Sync flushes OS buffers to disk before closing.
	if err := fs.f.Sync(); err != nil {
		fs.f.Close()
		return err
	}
	return fs.f.Close()
}
