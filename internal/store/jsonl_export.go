package store

import (
	"context"
	"fmt"
	"os"
)

// ExportSessionAsJSONL writes a session's messages to a JSONL file at
// outputPath. The write is atomic — data is written to a temp file first,
// then renamed. The app never reads JSONL files; this is a pure export.
func ExportSessionAsJSONL(ctx context.Context, store SessionStore, id, outputPath string) error {
	ch, errCh := store.ExportMessages(ctx, id)

	tmpPath := outputPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("store: create temp jsonl: %w", err)
	}

	var writeErr error
	for line := range ch {
		if _, err := f.Write(line); err != nil {
			writeErr = err
			break
		}
	}
	closeErr := f.Close()

	// Drain remaining messages so the export goroutine can exit cleanly.
	for range ch {
	}

	// Check export errors after draining.
	select {
	case err := <-errCh:
		if err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("store: export messages: %w", err)
		}
	default:
	}

	if writeErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("store: write jsonl: %w", writeErr)
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
