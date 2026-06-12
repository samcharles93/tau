package registry

import (
	"testing"

	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/skills"
)

func TestDiscover(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()

	client := bus.Client("test-registry")
	defer client.Close()

	reg := New("/nonexistent", client)
	reg.Discover()

	// Should have built-in commands (at minimum).
	cmds := reg.All()
	if len(cmds) == 0 {
		t.Fatal("expected built-in commands after Discover")
	}

	// Verify key commands exist.
	want := []string{"exit", "model", "new", "settings", "session", "reasoning"}
	for _, name := range want {
		if _, ok := reg.Lookup(name); !ok {
			t.Errorf("missing built-in command %q", name)
		}
	}
}

func TestLookup(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()

	client := bus.Client("test-registry")
	defer client.Close()

	reg := New("/nonexistent", client)
	reg.Discover()

	// Known command.
	cmd, ok := reg.Lookup("model")
	if !ok {
		t.Fatal("expected model command")
	}
	if cmd.Name != "model" {
		t.Errorf("Name = %q, want model", cmd.Name)
	}
	if cmd.Label != "/model" {
		t.Errorf("Label = %q, want /model", cmd.Label)
	}
	if !cmd.AcceptsArgs {
		t.Error("model should accept args")
	}

	// Unknown command.
	if _, ok := reg.Lookup("nonexistent"); ok {
		t.Error("expected false for unknown command")
	}
}

func TestMergeSkills(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()

	client := bus.Client("test-registry")
	defer client.Close()

	reg := New("/nonexistent", client)
	reg.Discover()

	// Merge a user-invocable skill.
	reg.MergeSkills([]*skills.Skill{
		{Name: "pdf", Description: "PDF processing", UserInvocable: true, Scope: skills.ScopeUser},
		{Name: "lint", Description: "Lint runner", UserInvocable: true, Scope: skills.ScopeProject},
		{Name: "secret-tool", Description: "Internal", UserInvocable: false, Scope: skills.ScopeUser},
		nil, // should be skipped
	})

	// User skill should appear with /skill: prefix.
	cmd, ok := reg.Lookup("user:pdf")
	if !ok {
		t.Fatal("expected user:pdf command")
	}
	if cmd.Label != "/skill:pdf" {
		t.Errorf("Label = %q, want /skill:pdf", cmd.Label)
	}
	if cmd.Description != "PDF processing" {
		t.Errorf("Description = %q, want 'PDF processing'", cmd.Description)
	}

	// Project skill.
	cmd, ok = reg.Lookup("project:lint")
	if !ok {
		t.Fatal("expected project:lint command")
	}
	if cmd.Label != "/skill:lint" {
		t.Errorf("Label = %q, want /skill:lint", cmd.Label)
	}

	// Non-user-invocable skill should be skipped.
	if _, ok := reg.Lookup("user:secret-tool"); ok {
		t.Error("non-user-invocable skill should not be registered")
	}

	// Nil skill should not panic.
	if _, ok := reg.Lookup(""); ok {
		t.Error("nil skill should not produce a command")
	}
}

func TestBuiltinsTakePrecedence(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()

	client := bus.Client("test-registry")
	defer client.Close()

	reg := New("/nonexistent", client)
	reg.Discover()

	// A skill named "model" should not override the built-in.
	reg.MergeSkills([]*skills.Skill{
		{Name: "model", Description: "custom model thing", UserInvocable: true, Scope: skills.ScopeUser},
	})

	cmd, ok := reg.Lookup("model")
	if !ok {
		t.Fatal("model command should still exist")
	}
	// Built-in description should win.
	if cmd.Description != "switch model" {
		t.Errorf("Description = %q, want 'switch model' (built-in wins)", cmd.Description)
	}
}

func TestPublishDoesNotPanic(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()

	pubClient := bus.Client("registry")
	defer pubClient.Close()

	reg := New("/nonexistent", pubClient)

	// Discover publishes via the bus — should not panic.
	reg.Discover()
	if len(reg.All()) == 0 {
		t.Error("expected non-empty command list after Discover")
	}

	// MergeSkills publishes via the bus — should not panic.
	reg.MergeSkills([]*skills.Skill{
		{Name: "test-skill", Description: "test", UserInvocable: true, Scope: skills.ScopeUser},
	})
	if _, ok := reg.Lookup("user:test-skill"); !ok {
		t.Error("expected user:test-skill after MergeSkills")
	}
}
