package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/stretchr/testify/require"
)

// writeAgentDefWithTools writes a minimal .agent.md file with an explicit
// tools list under dir/<name>.agent.md.
func writeAgentDefWithTools(t *testing.T, dir, name string, tools []string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	var toolsYAML strings.Builder
	for _, tl := range tools {
		toolsYAML.WriteString("\n  - ")
		toolsYAML.WriteString(tl)
	}
	content := "---\nname: " + name + "\ndescription: test agent\ntools:" + toolsYAML.String() + "\n---\n\nBody.\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".agent.md"), []byte(content), 0o644))
}

// newRegistryWithTools returns a tool registry with a no-op tool registered
// under each given name, so buildToolDefs() has something real to filter.
func newRegistryWithTools(t *testing.T, names ...string) *tools.Registry {
	t.Helper()
	reg := tools.NewRegistry()
	for _, name := range names {
		require.NoError(t, reg.Register(tools.Tool{
			Schema: tools.Schema{Name: name, Parameters: json.RawMessage(`{"type":"object"}`)},
			Execute: func(context.Context, json.RawMessage, tools.UIBridge) (tools.Result, error) {
				return tools.Result{}, nil
			},
		}))
	}
	return reg
}

// activeToolNames returns the set of tool names buildToolDefs() would
// currently send to the model, given the coordinator's live allowedTools
// filter.
func activeToolNames(c *Coordinator) map[string]bool {
	out := make(map[string]bool)
	for _, def := range c.buildToolDefs() {
		out[def.Function.Name] = true
	}
	return out
}

func waitForAgentActivated(t *testing.T, sub interface{ Events() <-chan chat.ChatEvent }, name string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-sub.Events():
			if n, ok := ev.(chat.ChatNotificationEvent); ok && n.Message == "agent activated: "+name {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for agent %q to activate", name)
		}
	}
}

// TestHandleRunAgentClampsDiscoveredAgentTools guards the Part B safety
// guard: a filesystem-discovered agent definition (untrusted — may be
// self-authored by the model) must never be able to grant itself a wider
// tool set than the session's current allowlist already permits.
func TestHandleRunAgentClampsDiscoveredAgentTools(t *testing.T) {
	projectDir := t.TempDir()
	writeAgentDefWithTools(t, filepath.Join(projectDir, ".agents", "agents"), "wide", []string{"bash", "write"})

	reg := newRegistryWithTools(t, "read", "grep", "bash", "write")
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: newTestBus(t),
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer:   noopStreamer{},
		Registry:   reg,
		ProjectDir: projectDir,
	})
	require.NoError(t, err)
	defer coordinator.Close()

	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	startTestSession(t, coordinator)

	// Simulate the session already being restricted (e.g. by an active
	// skill) to a narrower tool set than the discovered agent requests.
	coordinator.SetAllowedTools([]string{"read", "grep"})

	require.NoError(t, coordinator.Send(chat.RunAgentCommand{
		SessionID:   "session-1",
		Name:        "project:wide",
		RequestedAt: time.Now().UTC(),
	}))
	waitForAgentActivated(t, sub, "wide")

	active := activeToolNames(coordinator)
	require.False(t, active["bash"], "discovered agent must not widen the allowlist to include bash")
	require.False(t, active["write"], "discovered agent must not widen the allowlist to include write")
	// The intersection of {read,grep} and {bash,write} is empty. Clamping
	// to an empty set must NOT call SetAllowedTools(empty) — that method
	// treats an empty list as "no restriction" and would grant everything,
	// exactly the escalation this guard exists to prevent. The correct
	// outcome is that the pre-existing allowlist survives untouched.
	require.True(t, active["read"], "the pre-existing allowlist must survive when the clamp result is empty")
	require.True(t, active["grep"], "the pre-existing allowlist must survive when the clamp result is empty")
}

// TestHandleRunAgentDiscoveredAgentOverlapKeepsIntersection is the
// non-degenerate case: when the discovered agent's requested tools
// partially overlap the current allowlist, the overlap survives.
func TestHandleRunAgentDiscoveredAgentOverlapKeepsIntersection(t *testing.T) {
	projectDir := t.TempDir()
	writeAgentDefWithTools(t, filepath.Join(projectDir, ".agents", "agents"), "partial", []string{"bash", "read"})

	reg := newRegistryWithTools(t, "read", "grep", "bash")
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: newTestBus(t),
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer:   noopStreamer{},
		Registry:   reg,
		ProjectDir: projectDir,
	})
	require.NoError(t, err)
	defer coordinator.Close()

	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	startTestSession(t, coordinator)
	coordinator.SetAllowedTools([]string{"read", "grep"})

	require.NoError(t, coordinator.Send(chat.RunAgentCommand{
		SessionID:   "session-1",
		Name:        "project:partial",
		RequestedAt: time.Now().UTC(),
	}))
	waitForAgentActivated(t, sub, "partial")

	active := activeToolNames(coordinator)
	require.True(t, active["read"], "read is in both sets, so it must survive the clamp")
	require.False(t, active["bash"], "bash was not in the current allowlist, so it must not be granted")
	require.False(t, active["grep"], "grep was not requested by the agent, so it must not remain active")
}

// TestHandleRunAgentDiscoveredAgentWithNoToolsLeavesAllowlistUntouched
// guards the other half of the escalation path: a discovered definition
// declaring NO tools list at all must not be treated as "grant everything"
// — that would be the single largest possible escalation.
func TestHandleRunAgentDiscoveredAgentWithNoToolsLeavesAllowlistUntouched(t *testing.T) {
	projectDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, ".agents", "agents"), 0o755))
	content := "---\nname: bare\ndescription: test agent with no tools list\n---\n\nBody.\n"
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".agents", "agents", "bare.agent.md"), []byte(content), 0o644))

	reg := newRegistryWithTools(t, "read", "grep", "bash", "write")
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: newTestBus(t),
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer:   noopStreamer{},
		Registry:   reg,
		ProjectDir: projectDir,
	})
	require.NoError(t, err)
	defer coordinator.Close()

	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	startTestSession(t, coordinator)
	coordinator.SetAllowedTools([]string{"read", "grep"})

	require.NoError(t, coordinator.Send(chat.RunAgentCommand{
		SessionID:   "session-1",
		Name:        "project:bare",
		RequestedAt: time.Now().UTC(),
	}))
	waitForAgentActivated(t, sub, "bare")

	active := activeToolNames(coordinator)
	require.True(t, active["read"], "the pre-existing allowlist must survive untouched")
	require.True(t, active["grep"], "the pre-existing allowlist must survive untouched")
	require.False(t, active["bash"], "a discovered agent's empty tools list must not open up bash")
	require.False(t, active["write"], "a discovered agent's empty tools list must not open up write")
}

// TestHandleRunAgentBuiltinToolsApplyDirectly confirms built-ins keep their
// pre-existing (unclamped) behaviour: a trusted built-in's tool list
// replaces the current allowlist as-is, exactly like before this change.
func TestHandleRunAgentBuiltinToolsApplyDirectly(t *testing.T) {
	reg := newRegistryWithTools(t, "read", "grep", "bash", "write", "edit", "find", "docs")
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: newTestBus(t),
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer: noopStreamer{},
		Registry: reg,
	})
	require.NoError(t, err)
	defer coordinator.Close()

	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	startTestSession(t, coordinator)
	// Start narrower than /plan's own declared tools: [read, find, grep, docs].
	coordinator.SetAllowedTools([]string{"read"})

	require.NoError(t, coordinator.Send(chat.RunAgentCommand{
		SessionID:   "session-1",
		Name:        "plan",
		RequestedAt: time.Now().UTC(),
	}))
	waitForAgentActivated(t, sub, "plan")

	// A built-in's own tool restriction applies as-is and can widen the
	// session's current allowlist — unchanged from before this change, and
	// the direct contrast with the discovered-agent tests above: trust in
	// a built-in (shipped, reviewed) is what makes this safe, and is
	// exactly what a filesystem-discovered definition does not get.
	active := activeToolNames(coordinator)
	require.True(t, active["find"], "a built-in must be able to widen the allowlist to its own declared tools")
	require.True(t, active["grep"], "a built-in must be able to widen the allowlist to its own declared tools")
	require.True(t, active["docs"], "a built-in must be able to widen the allowlist to its own declared tools")
	require.False(t, active["bash"], "plan does not declare bash, so it must not be active")
}
