package store

import (
	"context"
	"fmt"
	"io"
	"os"
)

// ExportSessionAsJSONL writes a session's messages to a JSONL file at
// outputPath. The write is atomic - data is written to a temp file first,
// then renamed. The app never reads JSONL files; this is a pure export.
func ExportSessionAsJSONL(ctx context.Context, store SessionStore, id, outputPath string) error {
	tmpPath := outputPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("store: create temp jsonl: %w", err)
	}

	streamErr := streamMessagesToJSONL(ctx, store, id, f)
	closeErr := f.Close()

	if streamErr != nil {
		os.Remove(tmpPath)
		return streamErr
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("store: close jsonl: %w", closeErr)
	}

	if err := os.Rename(tmpPath, outputPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("store: rename temp jsonl: %w", err)
	}

	return nil
}

// streamMessagesToJSONL writes every exported message line to w. It derives a
// cancellable context from ctx so that a write error cancels the
// ExportMessages producer. Without the cancel, a write error would leave the
// producer blocked on a full output channel while the drain loop below waits
// for that channel to close - a permanent deadlock.
func streamMessagesToJSONL(ctx context.Context, store SessionStore, id string, w io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ch, errCh := store.ExportMessages(ctx, id)

	var writeErr error
	for line := range ch {
		if _, err := w.Write(line); err != nil {
			writeErr = err
			// Stop the producer; the drain loop below then terminates once
			// the producer exits via its ctx.Done() path and closes ch.
			cancel()
			break
		}
	}

	// Drain remaining messages so the export goroutine can exit cleanly.
	for range ch {
	}

	if writeErr != nil {
		return fmt.Errorf("store: write jsonl: %w", writeErr)
	}

	// The producer closes errCh before ch, so once the drain above has
	// finished, any export error is already buffered here.
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("store: export messages: %w", err)
		}
	default:
	}
	return nil
}
