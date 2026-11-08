package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// blockingExportStore mimics SQLiteStore.ExportMessages: a buffered channel
// (cap 64) fed by a goroutine that exits via ctx.Done().
type blockingExportStore struct {
	SessionStore
	lines int
	// cancelled, when non-nil, is set if the producer exits via ctx.Done().
	cancelled *atomic.Bool
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
				if s.cancelled != nil {
					s.cancelled.Store(true)
				}
				errs <- ctx.Err()
				return
			}
		}
	}()
	return out, errs
}

// blockUntilCancelStore blocks the producer until ctx is cancelled, then
// reports ctx.Err() - simulates a slow export interrupted mid-stream.
type blockUntilCancelStore struct {
	SessionStore
}

func (s *blockUntilCancelStore) ExportMessages(ctx context.Context, _ string) (<-chan []byte, <-chan error) {
	out := make(chan []byte, 64)
	errs := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errs)
		<-ctx.Done()
		errs <- ctx.Err()
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

// failAfterWriter lets the first n writes succeed, then fails with err -
// simulates a mid-stream write error (e.g. disk full) partway through the
// export rather than on the very first line.
type failAfterWriter struct {
	n      int
	err    error
	writes int
}

func (w *failAfterWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes > w.n {
		return 0, w.err
	}
	return len(p), nil
}

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

// A write error partway through the stream (not on the first line) must
// cancel the producer, drain the channel, and return the write error
// promptly - with more lines pending than the channel buffer holds.
func TestStreamMessagesToJSONL_MidStreamWriteErrorCancelsProducer(t *testing.T) {
	var cancelled atomic.Bool
	st := &blockingExportStore{lines: 500, cancelled: &cancelled}
	w := &failAfterWriter{n: 10, err: errors.New("disk full")}

	done := make(chan error, 1)
	go func() {
		done <- streamMessagesToJSONL(context.Background(), st, "s", w)
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "disk full") {
			t.Fatalf("expected the write error to be returned, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("streamMessagesToJSONL deadlocked on a mid-stream write error")
	}

	if w.writes != 11 {
		t.Fatalf("expected the writer to fail on the 11th write, got %d writes", w.writes)
	}
	// The drain loop only finishes once the producer has closed the channel,
	// so a returned result already proves the drain; cancelled proves the
	// producer exited via its ctx.Done() path rather than blocking forever.
	if !cancelled.Load() {
		t.Fatal("producer was not cancelled after the mid-stream write error")
	}
}

// A failing export stream must surface the error and leave neither the
// output file nor the temp file behind.
func TestExportSessionAsJSONL_StreamErrorCleansTemp(t *testing.T) {
	st := &errExportStore{err: errors.New("session not found")}
	outputPath := filepath.Join(t.TempDir(), "export.jsonl")

	err := ExportSessionAsJSONL(context.Background(), st, "missing", outputPath)
	if err == nil || !strings.Contains(err.Error(), "session not found") {
		t.Fatalf("expected the export error to be returned, got %v", err)
	}
	if _, statErr := os.Stat(outputPath); !os.IsNotExist(statErr) {
		t.Fatalf("no output file should exist after a failed export, stat err: %v", statErr)
	}
	if _, statErr := os.Stat(outputPath + ".tmp"); !os.IsNotExist(statErr) {
		t.Fatalf("temp file should be cleaned up after a failed export, stat err: %v", statErr)
	}
}

// Cancelling the context while the export is blocked mid-stream must return
// promptly and clean up the temp file.
func TestExportSessionAsJSONL_CancelMidStream(t *testing.T) {
	st := &blockUntilCancelStore{}
	ctx, cancel := context.WithCancel(context.Background())
	outputPath := filepath.Join(t.TempDir(), "export.jsonl")

	done := make(chan error, 1)
	go func() {
		done <- ExportSessionAsJSONL(ctx, st, "s", outputPath)
	}()

	time.Sleep(50 * time.Millisecond) // let the export block inside the producer
	cancel()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("expected context.Canceled to surface, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ExportSessionAsJSONL did not return after mid-stream cancellation")
	}

	if _, statErr := os.Stat(outputPath); !os.IsNotExist(statErr) {
		t.Fatalf("no output file should exist after cancellation, stat err: %v", statErr)
	}
	if _, statErr := os.Stat(outputPath + ".tmp"); !os.IsNotExist(statErr) {
		t.Fatalf("temp file should be cleaned up after cancellation, stat err: %v", statErr)
	}
}
