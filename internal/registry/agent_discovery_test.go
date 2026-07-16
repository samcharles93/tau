package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samcharles93/tau/internal/eventbus"
)

func writeAgentFile(t *testing.T, dir, name, description string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writing agent fixture: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, name+".agent.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing agent fixture: %v", err)
	}
}

// TestDiscoveredAgentAppearsWithProjectPrefix guards Part A of the
// agent-authored-subagent feature: a definition dropped under
// <cwd>/.agents/agents/ must show up in the registry with the project:
// prefix (execution is wired separately - see agentCommands' doc comment).
func TestDiscoveredAgentAppearsWithProjectPrefix(t *testing.T) {
	cwd := t.TempDir()
	writeAgentFile(t, filepath.Join(cwd, ".agents", "agents"), "reviewer", "Reviews code")

	bus := eventbus.New()
	defer bus.Close()
	client := bus.Client("test-registry")
	defer client.Close()

	reg := New(cwd, client)
	reg.Discover()

	cmd, ok := reg.Lookup("project:reviewer")
	if !ok {
		t.Fatal("expected project:reviewer after Discover")
	}
	if cmd.Label != "/project:reviewer" {
		t.Errorf("Label = %q, want /project:reviewer", cmd.Label)
	}
	if cmd.Description != "Reviews code" {
		t.Errorf("Description = %q, want 'Reviews code'", cmd.Description)
	}
}

// TestDiscoveredAgentNameCollisionBuiltinWins guards against a
// filesystem-discovered agent shadowing (or being confused with) a built-in
// of the same name - the built-in "plan" command must keep its own
// description, and the discovered file must not silently overwrite it.
func TestDiscoveredAgentNameCollisionBuiltinWins(t *testing.T) {
	cwd := t.TempDir()
	writeAgentFile(t, filepath.Join(cwd, ".agents", "agents"), "plan", "a fake plan agent")

	bus := eventbus.New()
	defer bus.Close()
	client := bus.Client("test-registry")
	defer client.Close()

	reg := New(cwd, client)
	reg.Discover()

	cmd, ok := reg.Lookup("plan")
	if !ok {
		t.Fatal("expected built-in plan command to exist")
	}
	if cmd.Description == "a fake plan agent" {
		t.Error("built-in 'plan' command was overwritten by a discovered agent definition")
	}

	// The discovered file must not appear under a prefixed name either,
	// since agentCommands skips names that collide with a built-in.
	if _, ok := reg.Lookup("project:plan"); ok {
		t.Error("did not expect project:plan to be registered when it collides with a built-in name")
	}
}

// TestNoAgentsDirIsNotAnError guards the common case: most projects have no
// .agents/agents/ directory at all, which must not produce an error or
// diagnostic-driven crash.
func TestNoAgentsDirIsNotAnError(t *testing.T) {
	cwd := t.TempDir()

	bus := eventbus.New()
	defer bus.Close()
	client := bus.Client("test-registry")
	defer client.Close()

	reg := New(cwd, client)
	reg.Discover()

	if len(reg.All()) == 0 {
		t.Fatal("expected built-in commands even with no .agents/agents directory")
	}
}
