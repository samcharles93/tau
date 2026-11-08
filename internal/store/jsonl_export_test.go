package store

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// blockingExportStore mimics SQLiteStore.ExportMessages: a buffered channel
// (cap 64) fed by a goroutine that exits via ctx.Done().
type blockingExportStore struct {
	SessionStore
	lines int
}

func (s *blockingExportStore) ExportMessages(ctx context.Context, _ string) (<-chan []byte, <-chan error) {
	out := make(chan []byte, 64)
	errs := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errs)
		for i := 0; i < s.lines; i++ {
			select {
			case out <- []byte("{\"role\":\"user\"}\n"):
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}
		}
	}()
	return out, errs
}

// errExportStore fails the export itself, like a missing session.
type errExportStore struct {
	SessionStore
	err error
}

func (s *errExportStore) ExportMessages(_ context.Context, _ string) (<-chan []byte, <-chan error) {
	out := make(chan []byte)
	errs := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errs)
		errs <- s.err
	}()
	return out, errs
}

type failWriter struct{ err error }

func (w failWriter) Write([]byte) (int, error) { return 0, w.err }

// A write error with more messages remaining than the channel buffer (64)
// must cancel the producer and return the write error promptly. Before the
// fix this deadlocked: the drain loop waited for a channel close that never
// came because the producer was blocked sending into the full channel.
func TestStreamMessagesToJSONL_WriteErrorNoDeadlock(t *testing.T) {
	st := &blockingExportStore{lines: 500}

	done := make(chan error, 1)
	go func() {
		done <- streamMessagesToJSONL(context.Background(), st, "s", failWriter{err: errors.New("disk full")})
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "disk full") {
			t.Fatalf("expected the write error to be returned, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("streamMessagesToJSONL deadlocked on write error")
	}
}

func TestStreamMessagesToJSONL_ExportsAllLines(t *testing.T) {
	st := &blockingExportStore{lines: 500}

	var buf bytes.Buffer
	if err := streamMessagesToJSONL(context.Background(), st, "s", &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.Count(buf.String(), "\n"); got != 500 {
		t.Fatalf("expected 500 lines, got %d", got)
	}
}

func TestStreamMessagesToJSONL_ExportErrorSurfaces(t *testing.T) {
	st := &errExportStore{err: errors.New("session not found")}

	err := streamMessagesToJSONL(context.Background(), st, "missing", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "session not found") {
		t.Fatalf("expected the export error to be returned, got %v", err)
	}
}

// A cancelled caller context must also stop the producer promptly.
func TestStreamMessagesToJSONL_CallerCancel(t *testing.T) {
	st := &blockingExportStore{lines: 500}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- streamMessagesToJSONL(ctx, st, "s", &bytes.Buffer{})
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("streamMessagesToJSONL did not return after caller cancellation")
	}
}
