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
	"github.com/samcharles93/tau/internal/eventbus"
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

// ============================================================================
// P2.6: Per-run tool filtering tests (two-tier: effectiveTools + allowedTools)
// ============================================================================

// TestEffectiveToolsCeilingRejectsOutOfSetToolCalls creates a coordinator
// with an effectiveTools ceiling and verifies that a tool outside the
// ceiling is rejected as "unknown tool" — the buildToolDefs filter means
// the LLM can't see it, and if it somehow calls it anyway (hallucination),
// the registry lookup fails with the standard unknown-tool path.
func TestEffectiveToolsCeilingRejectsOutOfSetToolCalls(t *testing.T) {
	reg := newRegistryWithTools(t, "read", "grep", "edit", "bash")
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus:          newTestBus(t),
		TokenSource:  func(context.Context, config.ProviderConfig) (string, error) { return "", nil },
		Streamer:     &unknownToolStreamer{toolName: "edit"},
		Registry:     reg,
		AllowedTools: []string{"read", "grep"},
	})
	require.NoError(t, err)
	defer coordinator.Close()

	// Verify buildToolDefs respects the ceiling.
	active := activeToolNames(coordinator)
	require.True(t, active["read"])
	require.True(t, active["grep"])
	// skill is in the effectiveTools ceiling but not registered in
	// this test's tool registry, so buildToolDefs can't return it.
	require.False(t, active["edit"], "edit is outside the ceiling")
	require.False(t, active["bash"], "bash is outside the ceiling")

	// Send a prompt — the streamer calls "edit" which IS registered but is
	// outside the ceiling (only read/grep are allowed). The execution-time
	// guard must reject it as unknown tool.
	sub := eventbus.Subscribe[chat.ChatEvent](coordinator.bus.Client("test-observer"))
	defer sub.Close()
	startTestSession(t, coordinator)
	require.NoError(t, coordinator.Send(chat.SubmitChatPromptCommand{
		SessionID:   "session-1",
		RequestID:   "request-1",
		Prompt:      "use missing",
		SubmittedAt: time.Now().UTC(),
	}))

	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-sub.Events():
			completed, ok := event.(chat.ChatToolExecutionCompletedEvent)
			if !ok {
				continue
			}
			require.Equal(t, "edit", completed.ToolName)
			require.Equal(t, "error", completed.Status)
			require.True(t, completed.IsError)
			require.Contains(t, completed.ResultSummary, "unknown tool")
			return
		case <-deadline:
			t.Fatal("timed out waiting for unknown tool completion event")
		}
	}
}

// TestModeIntersectionWithinCeiling verifies that a mode (via
// SetAllowedTools) can only narrow within the effectiveTools ceiling
// and never widens beyond it.
func TestModeIntersectionWithinCeiling(t *testing.T) {
	reg := newRegistryWithTools(t, "read", "grep", "edit", "bash", "write")
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus:          newTestBus(t),
		TokenSource:  func(context.Context, config.ProviderConfig) (string, error) { return "", nil },
		Streamer:     noopStreamer{},
		Registry:     reg,
		AllowedTools: []string{"read", "grep", "edit"},
	})
	require.NoError(t, err)
	defer coordinator.Close()

	// Initial state: ceiling only, no active mode.
	active := activeToolNames(coordinator)
	require.True(t, active["read"])
	require.True(t, active["grep"])
	require.True(t, active["edit"])
	require.False(t, active["bash"])
	require.False(t, active["write"])

	// Activate a mode that requests ["read", "bash"]. Bash is outside the
	// ceiling, so it should be excluded. Read + skill survive.
	coordinator.SetAllowedTools([]string{"read", "bash"})
	active = activeToolNames(coordinator)
	require.True(t, active["read"], "read is in both sets")
	// skill ceiling entry not visible because skill tool not in registry
	require.False(t, active["bash"], "bash is outside the ceiling, must not be granted")
	require.False(t, active["grep"], "grep was not in the mode list")
	require.False(t, active["edit"], "edit was not in the mode list")

	// Mode exit: revert to ceiling.
	coordinator.SetAllowedTools(nil)
	active = activeToolNames(coordinator)
	require.True(t, active["read"])
	require.True(t, active["grep"])
	require.True(t, active["edit"])
	require.False(t, active["bash"])
}

// TestPluginRegistrationMidRunBlockedByCeiling verifies that a new plugin
// tool registered mid-session is invisible to the LLM if it falls outside
// the effectiveTools ceiling, and visible if inside.
func TestPluginRegistrationMidRunBlockedByCeiling(t *testing.T) {
	reg := newRegistryWithTools(t, "read", "grep")
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus:          newTestBus(t),
		TokenSource:  func(context.Context, config.ProviderConfig) (string, error) { return "", nil },
		Streamer:     noopStreamer{},
		Registry:     reg,
		AllowedTools: []string{"read", "grep"},
	})
	require.NoError(t, err)
	defer coordinator.Close()

	// Register a new plugin tool "myplugin__extra" mid-run.
	require.NoError(t, reg.RegisterPluginTool("myplugin", tools.PluginToolDef{Name: "extra"}))

	active := activeToolNames(coordinator)
	// "myplugin__extra" was NOT in the effectiveTools ceiling originally.
	// Since the ceiling is immutable, it should be blocked.
	require.False(t, active["myplugin__extra"], "plugin tool outside ceiling must be blocked")
	require.True(t, active["read"])
	require.True(t, active["grep"])

	// Register a plugin tool "myplugin__read" that overlaps "read" in name.
	// The ceiling allows "read", and the plugin tool's sanitized name is
	// "myplugin__read" — this is NOT the same as "read", so it's also blocked.
	require.NoError(t, reg.RegisterPluginTool("myplugin", tools.PluginToolDef{Name: "read"}))
	active = activeToolNames(coordinator)
	require.False(t, active["myplugin__read"], "plugin tool with sanitized name must be blocked by ceiling")
}

// TestPluginRegistrationNoCeilingPassesThrough verifies that without a
// ceiling, plugin tools registered mid-run ARE visible.
func TestPluginRegistrationNoCeilingPassesThrough(t *testing.T) {
	reg := newRegistryWithTools(t, "read", "grep")
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus:         newTestBus(t),
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) { return "", nil },
		Streamer:    noopStreamer{},
		Registry:    reg,
		// No AllowedTools — unrestricted.
	})
	require.NoError(t, err)
	defer coordinator.Close()

	require.NoError(t, reg.RegisterPluginTool("myplugin", tools.PluginToolDef{Name: "extra"}))

	active := activeToolNames(coordinator)
	require.True(t, active["myplugin__extra"], "plugin tool must be visible when no ceiling")
	require.True(t, active["read"])
}

// TestAgentToolRemovalCutsSpawnCapability verifies that removing "agent"
// from the effectiveTools ceiling makes it invisible to the LLM via
// buildToolDefs, which is the mechanism for cutting spawn capability
// down the tree (decision 8).
func TestAgentToolRemovalCutsSpawnCapability(t *testing.T) {
	// Register "agent" as a real tool so it CAN be filtered.
	reg := newRegistryWithTools(t, "read", "grep", "agent")

	// Ceiling includes "agent".
	coordWithAgent, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus:          newTestBus(t),
		TokenSource:  func(context.Context, config.ProviderConfig) (string, error) { return "", nil },
		Streamer:     noopStreamer{},
		Registry:     reg,
		AllowedTools: []string{"read", "agent"},
	})
	require.NoError(t, err)
	defer coordWithAgent.Close()

	activeWith := activeToolNames(coordWithAgent)
	require.True(t, activeWith["agent"], "agent tool must be visible when in ceiling")
	require.True(t, activeWith["read"])

	// Ceiling excludes "agent".
	coordWithoutAgent, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus:          newTestBus(t),
		TokenSource:  func(context.Context, config.ProviderConfig) (string, error) { return "", nil },
		Streamer:     noopStreamer{},
		Registry:     reg,
		AllowedTools: []string{"read"},
	})
	require.NoError(t, err)
	defer coordWithoutAgent.Close()

	activeWithout := activeToolNames(coordWithoutAgent)
	require.False(t, activeWithout["agent"], "agent tool must NOT be visible when excluded from ceiling")
	require.True(t, activeWithout["read"])
}

// TestSetAllowedToolsEmptyRevertsToCeiling verifies that mode exit
// (empty or nil list) reverts allowedTools to nil, restoring the
// ceiling-only filter — not the full unrestricted registry.
func TestSetAllowedToolsEmptyRevertsToCeiling(t *testing.T) {
	reg := newRegistryWithTools(t, "read", "grep", "bash", "write")
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus:          newTestBus(t),
		TokenSource:  func(context.Context, config.ProviderConfig) (string, error) { return "", nil },
		Streamer:     noopStreamer{},
		Registry:     reg,
		AllowedTools: []string{"read", "grep"},
	})
	require.NoError(t, err)
	defer coordinator.Close()

	// Start unrestricted (ceiling only).
	active := activeToolNames(coordinator)
	require.True(t, active["read"])
	require.True(t, active["grep"])
	require.False(t, active["bash"])

	// Activate a mode that narrows to ["read"].
	coordinator.SetAllowedTools([]string{"read"})
	active = activeToolNames(coordinator)
	require.True(t, active["read"])
	require.False(t, active["grep"], "grep was not in the mode list")

	// Empty list: should revert to ceiling, NOT full registry.
	coordinator.SetAllowedTools([]string{})
	active = activeToolNames(coordinator)
	require.True(t, active["read"])
	require.True(t, active["grep"])
	require.False(t, active["bash"], "empty SetAllowedTools must not grant bash outside the ceiling")
	require.False(t, active["write"], "empty SetAllowedTools must not grant write outside the ceiling")

	// Nil list: same behaviour.
	coordinator.SetAllowedTools(nil)
	active = activeToolNames(coordinator)
	require.True(t, active["read"])
	require.True(t, active["grep"])
	require.False(t, active["bash"])
}

// TestAllowedToolsConstructorDefaultsToUnrestricted verifies that when
// no AllowedTools is passed to NewCoordinator, effectiveTools is nil
// (unrestricted) and all tools are visible.
func TestAllowedToolsConstructorDefaultsToUnrestricted(t *testing.T) {
	reg := newRegistryWithTools(t, "read", "grep", "bash")
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus:         newTestBus(t),
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) { return "", nil },
		Streamer:    noopStreamer{},
		Registry:    reg,
		// No AllowedTools — nil slice.
	})
	require.NoError(t, err)
	defer coordinator.Close()

	active := activeToolNames(coordinator)
	require.True(t, active["read"])
	require.True(t, active["grep"])
	require.True(t, active["bash"])
	// No "skill" in the map because it was never registered; it's only
	// auto-added to non-empty ceilings.
	require.False(t, active["skill"], "skill is not auto-added when ceiling is unrestricted")
}

// TestAllowedToolsConstructorEmptySliceIsNil verifies that an empty slice
// is treated as nil (unrestricted).
func TestAllowedToolsConstructorEmptySliceIsNil(t *testing.T) {
	reg := newRegistryWithTools(t, "read", "grep", "bash")
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus:          newTestBus(t),
		TokenSource:  func(context.Context, config.ProviderConfig) (string, error) { return "", nil },
		Streamer:     noopStreamer{},
		Registry:     reg,
		AllowedTools: []string{},
	})
	require.NoError(t, err)
	defer coordinator.Close()

	active := activeToolNames(coordinator)
	require.True(t, active["read"])
	require.True(t, active["grep"])
	require.True(t, active["bash"])
}

// TestSkillIsNotAutoInjectedAtConstruction verifies that "skill" is no longer
// hardcoded into the effective tools ceiling. It must be explicitly declared
// in the spec's or spawn's tools list to participate in attenuation.
func TestSkillIsNotAutoInjectedAtConstruction(t *testing.T) {
	// "skill" IS in the registry but NOT in AllowedTools — it should be invisible.
	reg := newRegistryWithTools(t, "read", "skill")
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus:          newTestBus(t),
		TokenSource:  func(context.Context, config.ProviderConfig) (string, error) { return "", nil },
		Streamer:     noopStreamer{},
		Registry:     reg,
		AllowedTools: []string{"read"}, // skill NOT in this list
	})
	require.NoError(t, err)
	defer coordinator.Close()

	active := activeToolNames(coordinator)
	require.True(t, active["read"])
	require.False(t, active["skill"], "skill must not be auto-injected into the ceiling")
}

// TestSkillIsPresentWhenExplicitlyAllowed verifies that when "skill" IS
// explicitly declared in AllowedTools, it is present in the active set.
func TestSkillIsPresentWhenExplicitlyAllowed(t *testing.T) {
	reg := newRegistryWithTools(t, "read", "skill")
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus:          newTestBus(t),
		TokenSource:  func(context.Context, config.ProviderConfig) (string, error) { return "", nil },
		Streamer:     noopStreamer{},
		Registry:     reg,
		AllowedTools: []string{"read", "skill"}, // skill explicitly included
	})
	require.NoError(t, err)
	defer coordinator.Close()

	active := activeToolNames(coordinator)
	require.True(t, active["read"])
	require.True(t, active["skill"], "skill must be present when explicitly included in the tools list")
}

// TestSkillNotInjectedBySetAllowedTools verifies that SetAllowedTools no longer
// hardcodes "skill" into the active filter.
func TestSkillNotInjectedBySetAllowedTools(t *testing.T) {
	reg := newRegistryWithTools(t, "read", "grep", "bash", "skill")
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus:          newTestBus(t),
		TokenSource:  func(context.Context, config.ProviderConfig) (string, error) { return "", nil },
		Streamer:     noopStreamer{},
		Registry:     reg,
		AllowedTools: []string{"read", "grep"}, // skill NOT in ceiling
	})
	require.NoError(t, err)
	defer coordinator.Close()

	// SetAllowedTools narrows to just ["read"] — skill should still NOT appear.
	coordinator.SetAllowedTools([]string{"read"})
	active := activeToolNames(coordinator)
	require.True(t, active["read"])
	require.False(t, active["grep"])
	require.False(t, active["skill"], "SetAllowedTools must not auto-inject skill")
}
