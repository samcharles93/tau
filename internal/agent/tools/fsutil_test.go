package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// TestRunWithContext_ReturnsCtxErrOnAlreadyExpired verifies that when the
// context is already done before runWithContext is called, no goroutine is
// spawned and the context error is returned immediately.
func TestRunWithContext_ReturnsCtxErrOnAlreadyExpired(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	spawned := make(chan struct{})
	err := runWithContext(ctx, func() error {
		close(spawned)
		return nil
	})

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}

	// The goroutine must NOT have been spawned because the context
	// was already expired. Give it a short grace period to prove the
	// negative.
	select {
	case <-spawned:
		t.Fatal("fn was spawned even though the context was already expired — goroutine leak")
	case <-time.After(50 * time.Millisecond):
		// Expected: fn was never called.
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

// noTempFilesLeft asserts the directory holds no leftover *.tmp files from a
// failed or completed atomic write.
func noTempFilesLeft(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("leftover temp file %q in %s", e.Name(), dir)
		}
	}
}

func TestWriteFileAtomic_Basic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	if err := writeFileAtomic(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("content = %q, want %q", data, "hello")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %o, want 644", info.Mode().Perm())
	}
	noTempFilesLeft(t, dir)
}

func TestWriteFileAtomic_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "out.txt")

	if err := writeFileAtomic(path, []byte("nested"), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "nested" {
		t.Fatalf("content = %q, want %q", data, "nested")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
}

// Overwriting an existing file replaces its content and mode atomically - the
// old content must never be partially visible, and no temp file may leak.
func TestWriteFileAtomic_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(path, []byte("old content"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeFileAtomic(path, []byte("new content"), 0o640); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new content" {
		t.Fatalf("content = %q, want %q", data, "new content")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o, want 640", info.Mode().Perm())
	}
	noTempFilesLeft(t, dir)
}

// When the rename fails (target is an existing directory), the temp file must
// be cleaned up and the original entry untouched.
func TestWriteFileAtomic_RenameFailureCleansUp(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "existing-dir")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}

	err := writeFileAtomic(target, []byte("data"), 0o644)
	if err == nil {
		t.Fatal("expected rename onto an existing directory to fail")
	}
	if info, statErr := os.Stat(target); statErr != nil || !info.IsDir() {
		t.Fatalf("target directory was modified: info=%v err=%v", info, statErr)
	}
	noTempFilesLeft(t, dir)
}

// A target inside a read-only directory fails at temp-file creation, leaving
// nothing behind. Skipped when running as root, where permission bits don't
// block writes.
func TestWriteFileAtomic_ReadOnlyDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission bits are not enforced")
	}
	dir := t.TempDir()
	ro := filepath.Join(dir, "ro")
	if err := os.Mkdir(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })

	err := writeFileAtomic(filepath.Join(ro, "out.txt"), []byte("x"), 0o644)
	if err == nil {
		t.Fatal("expected failure writing into a read-only directory")
	}
	noTempFilesLeft(t, ro)
}

func TestWriteFileAtomicWithContext_Cancelled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := writeFileAtomicWithContext(ctx, path, []byte("x"), 0o644)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("file must not exist after a cancelled write: %v", statErr)
	}
}

func TestWriteFileAtomicWithContext_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	if err := writeFileAtomicWithContext(context.Background(), path, []byte("ctx"), 0o644); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ctx" {
		t.Fatalf("content = %q, want %q", data, "ctx")
	}
}
