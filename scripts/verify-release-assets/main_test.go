package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samcharles93/tau/internal/updater"
)

// writeCompleteMatrix populates dir with an archive for every supported
// target plus a checksums.txt entry recording that archive's real SHA-256,
// all at the given tag. Real hashes (not placeholders) so tests exercise
// run()'s actual hash-comparison path, not just presence checks.
func writeCompleteMatrix(t *testing.T, dir, tag string) {
	t.Helper()

	var checksums strings.Builder
	for _, target := range updater.SupportedTargets() {
		name, err := updater.ArchiveName(tag, target.OS, target.Arch)
		if err != nil {
			t.Fatalf("ArchiveName(%s, %s, %s): %v", tag, target.OS, target.Arch, err)
		}
		content := []byte("fake archive for " + name)
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatalf("write archive %s: %v", name, err)
		}
		sum := sha256.Sum256(content)
		fmt.Fprintf(&checksums, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(checksums.String()), 0o644); err != nil {
		t.Fatalf("write checksums.txt: %v", err)
	}
}

func TestRunPassesForCompleteMatrix(t *testing.T) {
	dir := t.TempDir()
	writeCompleteMatrix(t, dir, "v1.2.3")

	if err := run(dir, "v1.2.3"); err != nil {
		t.Fatalf("run() = %v, want nil", err)
	}
}

func TestRunFailsWhenTagEmpty(t *testing.T) {
	if err := run(t.TempDir(), ""); err == nil {
		t.Fatal("run() = nil, want error for empty tag")
	}
}

func TestRunFailsWhenArchiveMissing(t *testing.T) {
	dir := t.TempDir()
	writeCompleteMatrix(t, dir, "v1.2.3")

	name, err := updater.ArchiveName("v1.2.3", "windows", "arm64")
	if err != nil {
		t.Fatalf("ArchiveName: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, name)); err != nil {
		t.Fatalf("remove archive: %v", err)
	}

	err = run(dir, "v1.2.3")
	if err == nil {
		t.Fatal("run() = nil, want error for missing archive")
	}
	if !strings.Contains(err.Error(), name) {
		t.Fatalf("run() error = %q, want it to name the missing archive %q", err, name)
	}
}

func TestRunFailsWhenChecksumEntryMissing(t *testing.T) {
	dir := t.TempDir()
	writeCompleteMatrix(t, dir, "v1.2.3")

	name, err := updater.ArchiveName("v1.2.3", "linux", "amd64")
	if err != nil {
		t.Fatalf("ArchiveName: %v", err)
	}

	checksumsPath := filepath.Join(dir, "checksums.txt")
	raw, err := os.ReadFile(checksumsPath)
	if err != nil {
		t.Fatalf("read checksums.txt: %v", err)
	}

	var kept []string
	for line := range strings.SplitSeq(strings.TrimRight(string(raw), "\n"), "\n") {
		if !strings.HasSuffix(line, "  "+name) {
			kept = append(kept, line)
		}
	}
	if err := os.WriteFile(checksumsPath, []byte(strings.Join(kept, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("rewrite checksums.txt: %v", err)
	}

	err = run(dir, "v1.2.3")
	if err == nil {
		t.Fatal("run() = nil, want error for missing checksum entry")
	}
	if !strings.Contains(err.Error(), name) {
		t.Fatalf("run() error = %q, want it to name %q", err, name)
	}
}

// TestRunFailsWhenArchiveContentDoesNotMatchChecksum guards against a real
// bug: run() used to only check that a checksums.txt entry existed for a
// filename, never that the recorded hash matched the archive's actual
// bytes, so a corrupted or stale-checksum archive passed silently.
func TestRunFailsWhenArchiveContentDoesNotMatchChecksum(t *testing.T) {
	dir := t.TempDir()
	writeCompleteMatrix(t, dir, "v1.2.3")

	name, err := updater.ArchiveName("v1.2.3", "linux", "amd64")
	if err != nil {
		t.Fatalf("ArchiveName: %v", err)
	}
	// Overwrite the archive after writeCompleteMatrix recorded its checksum,
	// simulating a corrupted upload or a stale checksums.txt entry.
	if err := os.WriteFile(filepath.Join(dir, name), []byte("corrupted contents"), 0o644); err != nil {
		t.Fatalf("corrupt archive %s: %v", name, err)
	}

	err = run(dir, "v1.2.3")
	if err == nil {
		t.Fatal("run() = nil, want error for a checksum/content mismatch")
	}
	if !strings.Contains(err.Error(), name) {
		t.Fatalf("run() error = %q, want it to name %q", err, name)
	}
}

func TestRunFailsWhenChecksumsFileMissing(t *testing.T) {
	dir := t.TempDir()

	err := run(dir, "v1.2.3")
	if err == nil {
		t.Fatal("run() = nil, want error when checksums.txt is absent")
	}
}
