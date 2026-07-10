package spec

import (
	"path/filepath"
	"testing"

	"github.com/samcharles93/tau/internal/skills"
)

func TestResolveBuiltinByBareName(t *testing.T) {
	def, ok := Resolve("plan", "")
	if !ok {
		t.Fatal("expected built-in 'plan' to resolve")
	}
	if def.SourcePath != "" {
		t.Errorf("SourcePath = %q, want empty for a built-in", def.SourcePath)
	}
}

func TestResolveDiscoveredProjectPrefixed(t *testing.T) {
	projectDir := t.TempDir()
	writeDef(t, filepath.Join(projectDir, ".agents", "agents"), "reviewer", "Reviews code")

	def, ok := Resolve("project:reviewer", projectDir)
	if !ok {
		t.Fatal("expected project:reviewer to resolve")
	}
	if def.Name != "reviewer" {
		t.Errorf("Name = %q, want reviewer", def.Name)
	}
	if def.SourcePath == "" {
		t.Error("expected SourcePath to be set for a discovered definition")
	}
}

func TestResolveDiscoveredUserPrefixed(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	writeDef(t, filepath.Join(homeDir, ".agents", "agents"), "reviewer", "Reviews code")

	def, ok := Resolve("user:reviewer", "")
	if !ok {
		t.Fatal("expected user:reviewer to resolve")
	}
	if def.Name != "reviewer" {
		t.Errorf("Name = %q, want reviewer", def.Name)
	}
}

func TestResolveBareNameNeverMatchesDiscovered(t *testing.T) {
	projectDir := t.TempDir()
	writeDef(t, filepath.Join(projectDir, ".agents", "agents"), "reviewer", "Reviews code")

	// A discovered definition must only be reachable by its prefixed name —
	// the bare name must never match it, even though nothing built-in is
	// named "reviewer". This mirrors internal/registry never listing a
	// discovered agent's bare name.
	if _, ok := Resolve("reviewer", projectDir); ok {
		t.Error("expected bare name to NOT match a discovered definition")
	}
}

func TestResolveUnknownReturnsFalse(t *testing.T) {
	if _, ok := Resolve("totally-made-up", ""); ok {
		t.Error("expected an unknown name to not resolve")
	}
	if _, ok := Resolve("project:totally-made-up", t.TempDir()); ok {
		t.Error("expected an unknown prefixed name to not resolve")
	}
}

func TestCommandPrefix(t *testing.T) {
	tests := []struct {
		scope skills.Scope
		want  string
	}{
		{skills.ScopeProject, "project:"},
		{skills.ScopeUser, "user:"},
		{skills.Scope(""), ""},
	}
	for _, tt := range tests {
		def := &Definition{Scope: tt.scope}
		if got := def.CommandPrefix(); got != tt.want {
			t.Errorf("CommandPrefix() for scope %q = %q, want %q", tt.scope, got, tt.want)
		}
	}
}
