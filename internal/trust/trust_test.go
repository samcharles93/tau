package trust

import (
	"os"
	"path/filepath"
	"testing"
)

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
		t.Error("hash did not change after editing the file — content binding is broken")
	}
}

func TestSave_WithoutLoadFails(t *testing.T) {
	s := &Store{}
	if err := s.Save(); err == nil {
		t.Error("Save on a Store not obtained via Load should fail (no path to write to)")
	}
}
