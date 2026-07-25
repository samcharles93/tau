package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/samcharles93/tau/internal/agent/tools"
)

func aliasTestCoordinator(t *testing.T, names ...string) *Coordinator {
	t.Helper()
	reg := tools.NewRegistry()
	for _, n := range names {
		if err := reg.Register(tools.Tool{
			Schema: tools.Schema{Name: n, Parameters: []byte(`{"type":"object"}`)},
			Source: "builtin",
			Execute: func(_ context.Context, _ json.RawMessage, _ tools.UIBridge) (tools.Result, error) {
				return tools.Result{Content: "ok"}, nil
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	return &Coordinator{registry: reg}
}

func TestResolveToolName_MapsKnownAliases(t *testing.T) {
	c := aliasTestCoordinator(t, "shell", "read", "grep")

	for alias, want := range map[string]string{
		"bash":      "shell",
		"run_shell": "shell",
		"read_file": "read",
		"search":    "grep",
	} {
		if got := c.resolveToolName(alias); got != want {
			t.Errorf("resolveToolName(%q) = %q, want %q", alias, got, want)
		}
	}
}

// An alias must never shadow a real tool of the same name. "ls" and "glob"
// are both aliases elsewhere and real registered tools here.
func TestResolveToolName_PrefersRegisteredName(t *testing.T) {
	c := aliasTestCoordinator(t, "shell", "ls")
	if got := c.resolveToolName("ls"); got != "ls" {
		t.Fatalf("a registered name must win over an alias, got %q", got)
	}
}

// An alias whose target is not registered must not be rewritten, or the model
// gets an error naming a tool it never called.
func TestResolveToolName_LeavesUnresolvableAliasAlone(t *testing.T) {
	c := aliasTestCoordinator(t, "read")
	if got := c.resolveToolName("bash"); got != "bash" {
		t.Fatalf("unresolvable alias should be left alone, got %q", got)
	}
}

func TestUnknownToolResult_ListsAvailableTools(t *testing.T) {
	c := aliasTestCoordinator(t, "shell", "read", "grep")

	res := c.unknownToolResult("frobnicate")
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	if !strings.Contains(res.Content, "unknown tool") {
		t.Fatalf("existing callers match on 'unknown tool', got: %s", res.Content)
	}
	for _, want := range []string{"shell", "read", "grep"} {
		if !strings.Contains(res.Content, want) {
			t.Fatalf("available tools should list %q, got: %s", want, res.Content)
		}
	}
}

// 6 malformed calls in the analysed corpus arrived with an empty name.
func TestUnknownToolResult_HandlesEmptyName(t *testing.T) {
	c := aliasTestCoordinator(t, "shell")

	res := c.unknownToolResult("")
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	if !strings.Contains(res.Content, "shell") {
		t.Fatalf("an unnamed call should still list available tools, got: %s", res.Content)
	}
}
