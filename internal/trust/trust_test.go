package trust

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// tmpLitter returns the leftover temp files matching the writeFileAtomic
// pattern (trust.yaml.*.tmp) in dir.
func tmpLitter(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "trust.yaml.*.tmp"))
	if err != nil {
		t.Fatalf("glob temp files: %v", err)
	}
	return matches
}

func TestLoad_MissingFileIsEmptyStore(t *testing.T) {
	s, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.TrustedSpecs) != 0 {
		t.Errorf("expected empty store, got %d entries", len(s.TrustedSpecs))
	}
	if s.IsTrusted("/some/project", "abc123") {
		t.Error("empty store must not trust anything")
	}
}

func TestTrustAndSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.Trust("/proj", "hash1", ModeHash)
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reloaded.IsTrusted("/proj", "hash1") {
		t.Error("trusted entry did not survive save/reload")
	}
	if reloaded.IsTrusted("/proj", "hash2") {
		t.Error("hash-mode entry incorrectly trusted a different hash")
	}
}

func TestIsTrusted_HashModeRequiresExactMatch(t *testing.T) {
	s := &Store{TrustedSpecs: []Entry{{ProjectPath: "/proj", SpecHash: "abc", TrustMode: ModeHash}}}
	if !s.IsTrusted("/proj", "abc") {
		t.Error("exact hash match should be trusted")
	}
	if s.IsTrusted("/proj", "def") {
		t.Error("changed hash should not be trusted under hash mode (spec was edited)")
	}
	if s.IsTrusted("/other", "abc") {
		t.Error("different project path should not be trusted")
	}
}

func TestIsTrusted_PathModeTrustsAnyHash(t *testing.T) {
	s := &Store{TrustedSpecs: []Entry{{ProjectPath: "/proj", SpecHash: "abc", TrustMode: ModePath}}}
	if !s.IsTrusted("/proj", "abc") {
		t.Error("original hash should be trusted")
	}
	if !s.IsTrusted("/proj", "totally-different-hash") {
		t.Error("path mode should trust any future hash change in the project")
	}
}

func TestTrust_SupersedesExistingEntryForSameProject(t *testing.T) {
	s := &Store{}
	s.Trust("/proj", "hash1", ModeHash)
	s.Trust("/proj", "hash2", ModePath)
	if len(s.TrustedSpecs) != 1 {
		t.Fatalf("expected 1 entry after re-trusting same project, got %d", len(s.TrustedSpecs))
	}
	if !s.IsTrusted("/proj", "anything") {
		t.Error("second Trust call (path mode) should have replaced the first")
	}
}

func TestHashFile_ChangesWithContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tau.agent.md")
	if err := os.WriteFile(p, []byte("original content"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	h1, err := HashFile(p)
	if err != nil {
		t.Fatalf("HashFile: %v", err)
	}
	if err := os.WriteFile(p, []byte("modified content"), 0o600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	h2, err := HashFile(p)
	if err != nil {
		t.Fatalf("HashFile after edit: %v", err)
	}
	if h1 == h2 {
		t.Error("hash did not change after editing the file - content binding is broken")
	}
}

func TestSave_WithoutLoadFails(t *testing.T) {
	s := &Store{}
	if err := s.Save(); err == nil {
		t.Error("Save on a Store not obtained via Load should fail (no path to write to)")
	}
}

func TestSave_NewFileIs0600AndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	osTempBefore, err := filepath.Glob(filepath.Join(os.TempDir(), "trust.yaml.*.tmp"))
	if err != nil {
		t.Fatalf("glob OS temp dir: %v", err)
	}

	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.Trust("/proj", "hash1", ModeHash)
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "trust.yaml"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("new trust.yaml mode = %o, want 0600 (approvals are security-relevant)", got)
	}
	if litter := tmpLitter(t, dir); len(litter) != 0 {
		t.Errorf("Save left temp files in the store dir: %v", litter)
	}
	osTempAfter, err := filepath.Glob(filepath.Join(os.TempDir(), "trust.yaml.*.tmp"))
	if err != nil {
		t.Fatalf("glob OS temp dir: %v", err)
	}
	if len(osTempAfter) != len(osTempBefore) {
		t.Errorf("temp file leaked into the OS temp dir (must stay in the store's own directory): before=%d after=%d",
			len(osTempBefore), len(osTempAfter))
	}
}

func TestSave_PreservesExistingFileMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "trust.yaml")
	if err := os.WriteFile(p, []byte("trusted_specs: []\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.Chmod(p, 0o640); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.Trust("/proj", "hash1", ModeHash)
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Errorf("existing file mode = %o after Save, want 0640 (mode must be preserved)", got)
	}
}

func TestSave_CrashMidWriteLeavesOldFileIntact(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.Trust("/proj", "hash1", ModeHash)
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Simulate a crash between writing the temp file and the rename: a
	// partial, unparseable temp file sits in the SAME directory while the
	// real trust.yaml still holds the last complete write.
	partial := filepath.Join(dir, "trust.yaml.123456.tmp")
	if err := os.WriteFile(partial, []byte("trusted_specs:\n  - project_path: /half-writt"), 0o600); err != nil {
		t.Fatalf("plant partial temp file: %v", err)
	}

	// The store must still load the old, complete file - the partial temp
	// file must never become visible as the store.
	reloaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load after simulated crash: %v (old file was corrupted)", err)
	}
	if len(reloaded.TrustedSpecs) != 1 || !reloaded.IsTrusted("/proj", "hash1") {
		t.Errorf("simulated crash changed the visible store: %+v", reloaded.TrustedSpecs)
	}

	// And the next Save still produces a complete, parseable file.
	reloaded.Trust("/proj2", "hash2", ModePath)
	if err := reloaded.Save(); err != nil {
		t.Fatalf("Save after simulated crash: %v", err)
	}
	final, err := Load(dir)
	if err != nil {
		t.Fatalf("reload after post-crash Save: %v", err)
	}
	if !final.IsTrusted("/proj", "hash1") || !final.IsTrusted("/proj2", "anything") {
		t.Errorf("entries lost across simulated crash + save: %+v", final.TrustedSpecs)
	}
	if len(final.TrustedSpecs) != 2 {
		t.Errorf("partial temp file leaked into the store: got %d entries, want 2", len(final.TrustedSpecs))
	}
}

func TestWriteFileAtomic_FailedRenameLeavesTargetIntact(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "trust.yaml")
	// A non-empty directory at the target path makes the final rename fail
	// deterministically - the stand-in for any error/crash before rename.
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "keep"), []byte("old"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := writeFileAtomic(target, []byte("new data"), 0o600); err == nil {
		t.Fatal("writeFileAtomic should fail when the rename cannot replace the target")
	}
	if _, err := os.Stat(filepath.Join(target, "keep")); err != nil {
		t.Error("failed atomic write modified the existing target")
	}
	if litter := tmpLitter(t, dir); len(litter) != 0 {
		t.Errorf("failed atomic write left temp files behind: %v", litter)
	}
}

func TestSave_ConcurrentSavesNeverTearFile(t *testing.T) {
	dir := t.TempDir()
	const workers = 16
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, err := Load(dir)
			if err != nil {
				errCh <- fmt.Errorf("load: %w", err)
				return
			}
			s.Trust(fmt.Sprintf("/proj-%d", i), fmt.Sprintf("hash-%d", i), ModeHash)
			if err := s.Save(); err != nil {
				errCh <- fmt.Errorf("save: %w", err)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent Save: %v", err)
	}

	// Every save was atomic, so the file must parse cleanly - never a torn
	// mix of two writers' YAML.
	final, err := Load(dir)
	if err != nil {
		t.Fatalf("Load after concurrent saves (torn write?): %v", err)
	}
	if len(final.TrustedSpecs) == 0 || len(final.TrustedSpecs) > workers {
		t.Errorf("unexpected entry count after concurrent saves: %d", len(final.TrustedSpecs))
	}
	for _, e := range final.TrustedSpecs {
		if e.ProjectPath == "" || e.SpecHash == "" {
			t.Errorf("torn entry in store: %+v", e)
		}
	}
	if litter := tmpLitter(t, dir); len(litter) != 0 {
		t.Errorf("concurrent saves left temp files behind: %v", litter)
	}
}
