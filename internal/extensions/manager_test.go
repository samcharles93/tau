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

func TestReloadCleansToolsRegisteredDuringUnloadHook(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	toolPath := filepath.Join(root, "tooler.lua")
	writeFile(t, toolPath, `
tau.register_tool({ name = "old_tool", description = "old" }, function(args)
  return "old"
end)
tau.on("manager_unload", function(ctx)
  tau.register_tool({ name = "stale_tool", description = "stale" }, function(args)
    return "stale"
  end)
end)
`)

	registry := tools.NewRegistry()
	manager := newTestManager(t, root, registry)
	require.NoError(t, manager.Load(context.Background()))
	_, ok := registry.Get("old_tool")
	require.True(t, ok)

	writeFile(t, toolPath, `
tau.register_tool({ name = "new_tool", description = "new" }, function(args)
  return "new"
end)
`)
	require.NoError(t, manager.Reload(context.Background()))
	_, ok = registry.Get("old_tool")
	require.False(t, ok)
	_, ok = registry.Get("stale_tool")
	require.False(t, ok)
	_, ok = registry.Get("new_tool")
	require.True(t, ok)
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

func TestLuaConfirmRoutesThroughUIBridge(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "confirm.lua"), `
tau.register_tool({ name = "needs_confirm", description = "asks" }, function(args)
  if tau.confirm("Continue?", "Question") then
    return "confirmed"
  end
  return "denied"
end)
`)
	registry := tools.NewRegistry()
	manager := newTestManager(t, root, registry)
	require.NoError(t, manager.Load(context.Background()))

	tool, ok := registry.Get("needs_confirm")
	require.True(t, ok)
	ui := &fakeUIBridge{confirmResult: true}
	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`), ui)
	require.NoError(t, err)
	require.Equal(t, "confirmed", result.Content)
	require.Equal(t, "Question", ui.title)
	require.Equal(t, "Continue?", ui.description)
}

func TestLuaConfirmUnsupportedWithoutUIBridge(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "confirm.lua"), `
tau.register_tool({ name = "needs_confirm", description = "asks" }, function(args)
  return tostring(tau.confirm("Continue?"))
end)
`)
	registry := tools.NewRegistry()
	manager := newTestManager(t, root, registry)
	require.NoError(t, manager.Load(context.Background()))

	tool, ok := registry.Get("needs_confirm")
	require.True(t, ok)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{}`), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), tools.ErrInteractiveUnsupported.Error())
}

func TestLuaAskRoutesThroughUIBridge(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "ask.lua"), `
tau.register_tool({ name = "needs_answer", description = "asks" }, function(args)
  return tau.ask("Name?", "Question")
end)
`)
	registry := tools.NewRegistry()
	manager := newTestManager(t, root, registry)
	require.NoError(t, manager.Load(context.Background()))

	tool, ok := registry.Get("needs_answer")
	require.True(t, ok)
	ui := &fakeUIBridge{inputResult: "Tau"}
	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`), ui)
	require.NoError(t, err)
	require.Equal(t, "Tau", result.Content)
	require.Equal(t, "Question", ui.title)
	require.Equal(t, "Name?", ui.placeholder)
}

func TestLuaRegisterCommandAndExecute(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "commands.lua"), `
tau.register_command({ name = "hello", description = "greets" }, function(args)
  return "hello " .. args.raw
end)
`)
	manager := newTestManager(t, root, tools.NewRegistry())
	require.NoError(t, manager.Load(context.Background()))

	snapshot := manager.Snapshot()
	require.Len(t, snapshot.Commands, 1)
	require.Equal(t, "hello", snapshot.Commands[0].Name)
	require.Equal(t, "greets", snapshot.Commands[0].Description)

	output, err := manager.ExecuteCommand(context.Background(), "hello", "sam", nil)
	require.NoError(t, err)
	require.Equal(t, "hello sam", output)
}

func TestDuplicateExtensionCommandsAreDiagnosed(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "alpha.lua"), `
tau.register_command({ name = "hello", description = "first" }, function(args)
  return "alpha"
end)
`)
	writeFile(t, filepath.Join(root, "bravo.lua"), `
tau.register_command({ name = "hello", description = "duplicate" }, function(args)
  return "bravo"
end)
`)
	manager := newTestManager(t, root, tools.NewRegistry())
	require.NoError(t, manager.Load(context.Background()))

	snapshot := manager.Snapshot()
	require.Len(t, snapshot.Commands, 1)
	require.Equal(t, "alpha", snapshot.Commands[0].ExtensionName)
	require.True(t, containsDiagnostic(snapshot.Diagnostics, `command "hello" is already registered`))

	output, err := manager.ExecuteCommand(context.Background(), "hello", "", nil)
	require.NoError(t, err)
	require.Equal(t, "alpha", output)
}

func TestReservedCommandCollisionIsDiagnosed(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "reloader.lua"), `
tau.register_command({ name = "reload", description = "shadow" }, function(args)
  return "shadow"
end)
`)
	manager, err := NewManager(Config{
		Sources:          []Source{{Root: root, Scope: ScopeUser}},
		ReservedCommands: []string{"/reload"},
		Registry:         tools.NewRegistry(),
	})
	require.NoError(t, err)
	require.NoError(t, manager.Load(context.Background()))

	snapshot := manager.Snapshot()
	require.Empty(t, snapshot.Commands)
	require.True(t, containsDiagnostic(snapshot.Diagnostics, `command "reload" is already registered by built-in command`))
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

type fakeUIBridge struct {
	confirmResult bool
	inputResult   string
	title         string
	description   string
	placeholder   string
}

func (f *fakeUIBridge) Confirm(_ context.Context, title, description string) (bool, error) {
	f.title = title
	f.description = description
	return f.confirmResult, nil
}

func (f *fakeUIBridge) Select(context.Context, string, []string) (string, error) {
	return "", tools.ErrInteractiveUnsupported
}

func (f *fakeUIBridge) Input(_ context.Context, title, placeholder string) (string, error) {
	f.title = title
	f.placeholder = placeholder
	return f.inputResult, nil
}

func (f *fakeUIBridge) Notify(string, string) {}
