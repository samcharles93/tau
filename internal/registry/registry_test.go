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

	// Built-in commands are always present regardless of custom command
	// directories. Verify the built-in set is non-empty.
	cmds := reg.All()
	if len(cmds) == 0 {
		t.Fatal("expected built-in commands after Discover, got 0")
	}
	// Spot-check a well-known built-in.
	if cmd, ok := reg.Lookup("model"); !ok {
		t.Error("expected built-in 'model' command")
	} else if cmd.AcceptsArgs != true {
		t.Errorf("model AcceptsArgs = %v, want true", cmd.AcceptsArgs)
	}
}

func TestLookup(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()

	client := bus.Client("test-registry")
	defer client.Close()

	reg := New("/nonexistent", client)
	reg.Discover()

	// Built-in commands are present after Discover.
	if _, ok := reg.Lookup("model"); !ok {
		t.Error("expected built-in 'model' command to be present")
	}
	// A truly non-existent command still returns false.
	if _, ok := reg.Lookup("nonexistent-cmd"); ok {
		t.Error("expected false for non-existent command")
	}

	// Merge a skill and verify it is found.
	reg.MergeSkills([]*skills.Skill{
		{Name: "pdf", Description: "PDF processing", UserInvocable: true, Scope: skills.ScopeUser},
	})
	cmd, ok := reg.Lookup("user:pdf")
	if !ok {
		t.Fatal("expected user:pdf after MergeSkills")
	}
	if cmd.Label != "/skill:pdf" {
		t.Errorf("Label = %q, want /skill:pdf", cmd.Label)
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

func TestSkillPrecedence(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()

	client := bus.Client("test-registry")
	defer client.Close()

	reg := New("/nonexistent", client)
	reg.Discover()

	// Merge a skill whose name collides with a built-in ("model").
	// Built-ins take precedence, so the skill description must not
	// overwrite the built-in.
	reg.MergeSkills([]*skills.Skill{
		{Name: "model", Description: "custom model thing", UserInvocable: true, Scope: skills.ScopeUser},
	})

	cmd, ok := reg.Lookup("model")
	if !ok {
		t.Fatal("model command should exist after MergeSkills")
	}
	// Built-in takes precedence: description should be the original.
	if cmd.Description != "switch model" {
		t.Errorf("Description = %q, want 'switch model' (built-in wins)", cmd.Description)
	}

	// Merging a non-colliding skill should work normally.
	reg.MergeSkills([]*skills.Skill{
		{Name: "pdf", Description: "PDF processing", UserInvocable: true, Scope: skills.ScopeUser},
	})
	cmd, ok = reg.Lookup("user:pdf")
	if !ok {
		t.Fatal("expected user:pdf after MergeSkills")
	}
	if cmd.Description != "PDF processing" {
		t.Errorf("Description = %q, want 'PDF processing'", cmd.Description)
	}
}

func TestPublishDoesNotPanic(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()

	pubClient := bus.Client("registry")
	defer pubClient.Close()

	reg := New("/nonexistent", pubClient)

	// Discover publishes via the bus — should not panic even with no commands.
	reg.Discover()

	// MergeSkills publishes via the bus — should not panic.
	reg.MergeSkills([]*skills.Skill{
		{Name: "test-skill", Description: "test", UserInvocable: true, Scope: skills.ScopeUser},
	})
	if _, ok := reg.Lookup("user:test-skill"); !ok {
		t.Error("expected user:test-skill after MergeSkills")
	}
}
