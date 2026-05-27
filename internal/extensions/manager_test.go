package extensions

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/stretchr/testify/require"
)

func TestManagerLoadsLuaHooksAndDiagnostics(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "hooks.lua"), `
seen = ""
tau.log("warn", "loading")
tau.on("session_start", function(ctx)
  seen = ctx.session_id
end)
tau.on("session_shutdown", function(ctx)
  if ctx.session_id ~= seen then error("wrong session") end
end)
tau.on("tool_call_completed", function(ctx)
  if ctx.tool_name ~= "read" then error("wrong tool") end
end)
`)

	registry := tools.NewRegistry()
	manager := newTestManager(t, root, registry)
	require.NoError(t, manager.Load(context.Background()))

	manager.Dispatch(EventSessionStart, map[string]any{"event": "session_start", "session_id": "s1"})
	manager.Dispatch(EventSessionShutdown, map[string]any{"event": "session_shutdown", "session_id": "s1"})
	manager.Dispatch(EventToolCallCompleted, map[string]any{"event": "tool_call_completed", "tool_name": "read"})

	snapshot := manager.Snapshot()
	require.Len(t, snapshot.Extensions, 1)
	require.True(t, containsDiagnostic(snapshot.Diagnostics, "loading"))
	require.False(t, containsDiagnostic(snapshot.Diagnostics, "wrong session"))
	require.False(t, containsDiagnostic(snapshot.Diagnostics, "wrong tool"))
}

func TestManagerRuntimeErrorsBecomeDiagnostics(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "bad-hook.lua"), `
tau.on("session_start", function(ctx)
  error("boom")
end)
`)

	manager := newTestManager(t, root, tools.NewRegistry())
	require.NoError(t, manager.Load(context.Background()))

	manager.Dispatch(EventSessionStart, map[string]any{"event": "session_start"})

	require.True(t, containsDiagnostic(manager.Snapshot().Diagnostics, "boom"))
}

func TestManagerUsesIndependentLuaStates(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "first.lua"), `
counter = 1
tau.register_tool({ name = "first_counter", description = "counter" }, function(args)
  return tostring(counter)
end)
`)
	writeFile(t, filepath.Join(root, "second.lua"), `
counter = 2
tau.register_tool({ name = "second_counter", description = "counter" }, function(args)
  return tostring(counter)
end)
`)

	registry := tools.NewRegistry()
	manager := newTestManager(t, root, registry)
	require.NoError(t, manager.Load(context.Background()))

	first, ok := registry.Get("first_counter")
	require.True(t, ok)
	second, ok := registry.Get("second_counter")
	require.True(t, ok)
	firstResult, err := first.Execute(context.Background(), json.RawMessage(`{}`), nil)
	require.NoError(t, err)
	secondResult, err := second.Execute(context.Background(), json.RawMessage(`{}`), nil)
	require.NoError(t, err)
	require.Equal(t, "1", firstResult.Content)
	require.Equal(t, "2", secondResult.Content)
}

func TestManagerFailingExtensionDoesNotBlockValidExtension(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "bad.lua"), `error("load failed")`)
	writeFile(t, filepath.Join(root, "good.lua"), `tau.log("info", "good")`)

	manager := newTestManager(t, root, tools.NewRegistry())
	require.NoError(t, manager.Load(context.Background()))

	snapshot := manager.Snapshot()
	require.Len(t, snapshot.Extensions, 1)
	require.Equal(t, "good", snapshot.Extensions[0].Name)
	require.True(t, containsDiagnostic(snapshot.Diagnostics, "load failed"))
}

func TestToolRegistrationExecutionAndReloadCleanup(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	toolPath := filepath.Join(root, "tooler.lua")
	writeFile(t, toolPath, `
tau.register_tool({
  name = "hello",
  description = "says hello",
  parameters = {
    type = "object",
    properties = { name = { type = "string" } }
  }
}, function(args)
  return { content = "hello " .. args.name }
end)
`)

	registry := tools.NewRegistry()
	manager := newTestManager(t, root, registry)
	require.NoError(t, manager.Load(context.Background()))

	tool, ok := registry.Get("hello")
	require.True(t, ok)
	require.Equal(t, "extension:tooler", tool.Source)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"name":"tau"}`), nil)
	require.NoError(t, err)
	require.Equal(t, "hello tau", result.Content)

	writeFile(t, toolPath, `
tau.register_tool({ name = "goodbye", description = "says goodbye" }, function(args)
  return "bye"
end)
`)
	require.NoError(t, manager.Reload(context.Background()))
	_, ok = registry.Get("hello")
	require.False(t, ok)
	_, ok = registry.Get("goodbye")
	require.True(t, ok)

	manager.Unload()
	_, ok = registry.Get("goodbye")
	require.False(t, ok)
}

func TestDuplicateToolCollisionDiagnosed(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "collide.lua"), `
tau.register_tool({ name = "read", description = "duplicate" }, function(args)
  return "nope"
end)
`)
	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(tools.Tool{
		Schema: tools.Schema{Name: "read", Description: "builtin"},
		Execute: func(context.Context, json.RawMessage, tools.UIBridge) (tools.Result, error) {
			return tools.Result{}, nil
		},
		Source: "builtin",
	}))

	manager := newTestManager(t, root, registry)
	require.NoError(t, manager.Load(context.Background()))

	require.Empty(t, manager.Snapshot().Extensions)
	require.True(t, containsDiagnostic(manager.Snapshot().Diagnostics, "already registered"))
}

func TestManagerHonorsDisabledConfig(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "disabled.lua"), `tau.log("info", "nope")`)

	registry := tools.NewRegistry()
	manager, err := NewManager(Config{
		Sources:  []Source{{Root: root, Scope: ScopeUser}},
		Disabled: []string{"disabled"},
		Registry: registry,
	})
	require.NoError(t, err)
	require.NoError(t, manager.Load(context.Background()))

	require.Empty(t, manager.Snapshot().Extensions)
	require.True(t, containsDiagnostic(manager.Snapshot().Diagnostics, "disabled by config"))
}

func TestReloadIfIdleRejectsBusyRuntime(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t, t.TempDir(), tools.NewRegistry())

	require.ErrorIs(t, manager.ReloadIfIdle(context.Background(), false), ErrReloadWhileBusy)
	require.NoError(t, manager.ReloadIfIdle(context.Background(), true))
}

func newTestManager(t *testing.T, root string, registry *tools.Registry) *Manager {
	t.Helper()
	manager, err := NewManager(Config{
		Sources:  []Source{{Root: root, Scope: ScopeUser}},
		Registry: registry,
	})
	require.NoError(t, err)
	return manager
}
