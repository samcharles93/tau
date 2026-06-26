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

	// No custom commands exist under /nonexistent, but Discover should
	// succeed without panicking and return an empty set.
	cmds := reg.All()
	if len(cmds) != 0 {
		t.Fatalf("expected 0 commands after Discover (no custom commands), got %d", len(cmds))
	}
}

func TestLookup(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()

	client := bus.Client("test-registry")
	defer client.Close()

	reg := New("/nonexistent", client)
	reg.Discover()

	// No builtins; lookup of a non-existent command returns false.
	if _, ok := reg.Lookup("model"); ok {
		t.Error("expected false for command not added via skills or custom dirs")
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

	// Merge a skill.
	reg.MergeSkills([]*skills.Skill{
		{Name: "model", Description: "custom model thing", UserInvocable: true, Scope: skills.ScopeUser},
	})

	cmd, ok := reg.Lookup("user:model")
	if !ok {
		t.Fatal("user:model command should exist after MergeSkills")
	}
	if cmd.Description != "custom model thing" {
		t.Errorf("Description = %q, want 'custom model thing'", cmd.Description)
	}

	// Merging again should keep the original (first registration wins).
	reg.MergeSkills([]*skills.Skill{
		{Name: "model", Description: "newer model thing", UserInvocable: true, Scope: skills.ScopeUser},
	})
	cmd, _ = reg.Lookup("user:model")
	if cmd.Description != "custom model thing" {
		t.Errorf("Description = %q, want 'custom model thing' (first wins)", cmd.Description)
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
