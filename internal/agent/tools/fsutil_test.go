package tools

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRunWithContext_ReturnsFnResult(t *testing.T) {
	err := runWithContext(context.Background(), func() error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sentinel := errors.New("boom")
	err = runWithContext(context.Background(), func() error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

// TestRunWithContext_ReturnsCtxErrOnTimeout is the core regression coverage
// for tau-6wa: fn blocks forever (simulating a hung filesystem call); the
// caller must get control back once ctx expires rather than waiting for fn.
func TestRunWithContext_ReturnsCtxErrOnTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	fnStarted := make(chan struct{})
	blockForever := make(chan struct{}) // deliberately never closed

	start := time.Now()
	err := runWithContext(ctx, func() error {
		close(fnStarted)
		<-blockForever
		return nil
	})
	elapsed := time.Since(start)

	<-fnStarted // make sure fn actually started (and is now hung) before checking the result

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("runWithContext took %v to return after the deadline, expected it to return promptly instead of waiting on the hung fn", elapsed)
	}
}
