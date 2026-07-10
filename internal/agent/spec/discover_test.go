package spec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samcharles93/tau/internal/skills"
)

func writeDef(t *testing.T, dir, name, description string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, name+agentFileSuffix), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestDiscoverFromDiskParsesValidDefinition(t *testing.T) {
	dir := t.TempDir()
	writeDef(t, dir, "reviewer", "Reviews code")

	defs, diags := DiscoverFromDisk([]Source{{Root: dir, Scope: skills.ScopeProject, Priority: projectSourcePriority}})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}
	if len(defs) != 1 {
		t.Fatalf("got %d definitions, want 1", len(defs))
	}
	if defs[0].Name != "reviewer" || defs[0].Description != "Reviews code" {
		t.Errorf("got %+v", defs[0])
	}
	if defs[0].Scope != skills.ScopeProject {
		t.Errorf("Scope = %v, want ScopeProject", defs[0].Scope)
	}
	if defs[0].SourcePath == "" {
		t.Error("expected SourcePath to be set")
	}
}

func TestDiscoverFromDiskSkipsInvalidFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.agent.md"), []byte("---\nname: broken\n---\nno description\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	defs, diags := DiscoverFromDisk([]Source{{Root: dir, Scope: skills.ScopeUser, Priority: userSourcePriority}})
	if len(defs) != 0 {
		t.Fatalf("got %d definitions, want 0 (invalid file must not parse)", len(defs))
	}
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(diags))
	}
}

func TestDiscoverFromDiskProjectWinsOverUser(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()
	writeDef(t, userDir, "reviewer", "user version")
	writeDef(t, projectDir, "reviewer", "project version")

	defs, _ := DiscoverFromDisk([]Source{
		{Root: userDir, Scope: skills.ScopeUser, Priority: userSourcePriority},
		{Root: projectDir, Scope: skills.ScopeProject, Priority: projectSourcePriority},
	})
	if len(defs) != 1 {
		t.Fatalf("got %d definitions, want 1", len(defs))
	}
	if defs[0].Description != "project version" {
		t.Errorf("Description = %q, want project version to win on collision", defs[0].Description)
	}
}

func TestDiscoverFromDiskMissingRootIsNotError(t *testing.T) {
	defs, diags := DiscoverFromDisk([]Source{{Root: filepath.Join(t.TempDir(), "does-not-exist"), Scope: skills.ScopeUser, Priority: userSourcePriority}})
	if len(defs) != 0 || len(diags) != 0 {
		t.Fatalf("got defs=%d diags=%d, want 0 and 0 for a missing source directory", len(defs), len(diags))
	}
}

func TestDefaultSourcesIncludesProjectRoot(t *testing.T) {
	cwd := t.TempDir()
	sources := DefaultSources(cwd)
	found := false
	for _, s := range sources {
		if s.Root == filepath.Join(cwd, ".agents", "agents") && s.Scope == skills.ScopeProject {
			found = true
		}
	}
	if !found {
		t.Errorf("DefaultSources(%q) = %+v, want a project source at <cwd>/.agents/agents", cwd, sources)
	}
}
