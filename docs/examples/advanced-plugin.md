# Example: Advanced Plugins — Lifecycle Hooks & Interactive Prompts

::: tip What "advanced" means here
Tau's plugin API doesn't let a plugin render arbitrary custom widgets into
the TUI — there's no "draw this box" call. What it *does* give a plugin is
real control over the agent's behavior (via lifecycle event hooks) and two
interactive surfaces that appear in the TUI/Web UI on demand:
`host.Confirm()` (yes/no) and `host.Input()` (free text). Combined, that's
enough to build guardrails, approval flows, and context-aware plugins — this
page shows how.
:::

If you haven't read the [Plugin SDK](/plugins) guide, start there for the
`Extension` interface basics. This page builds a `guardrail` plugin that:

1. Blocks a dangerous shell command unless the user explicitly confirms it.
2. Injects an extra system-prompt instruction and a tracing header into every
   LLM call.
3. Notifies the user when a new session starts.

## Declaring the right capabilities

```go
func (p *GuardrailPlugin) Capabilities() []string {
    return []string{pluginapi.CapabilityEvents, pluginapi.CapabilityInteractive}
}
```

This plugin has no slash commands or tools of its own — it only reacts to
lifecycle events and prompts the user — so it omits `CapabilityCommands` and
`CapabilityTools`. Tau skips calling `RunCommand`/`Tools`/`ExecuteTool` on it
entirely, saving a gRPC round-trip per turn.

## Routing events

`DispatchEvent` receives every event as a string plus a typed `payload`
oneof. The pattern is a switch on `event`, using the matching payload getter:

```go
func (p *GuardrailPlugin) DispatchEvent(
    ctx context.Context, event string, sessionID string, payload *pluginapi.EventPayload,
) *pluginapi.EventResponse {
    switch event {
    case "session_start":
        return p.onSessionStart(ctx, payload.GetSession())
    case "before_llm_call":
        return p.onBeforeLLMCall(payload.GetBeforeLlmCall())
    case "before_tool_exec":
        return p.onBeforeToolExec(ctx, payload.GetBeforeToolExec())
    default:
        return nil // no opinion on this event
    }
}
```

Returning `nil` is always safe — it tells tau "proceed as normal." You only
need to return an `*EventResponse` for events you actually want to influence.

## Blocking a tool call pending confirmation

`before_tool_exec` fires immediately before any tool executes — built-in,
plugin-provided, or MCP-bridged. This is where you'd implement an approval
gate:

```go
func (p *GuardrailPlugin) onBeforeToolExec(ctx context.Context, call *pluginapi.ToolCallPayload) *pluginapi.EventResponse {
    if call.ToolName != "shell_execute" {
        return nil
    }

    var args struct {
        Command string `json:"command"`
    }
    _ = json.Unmarshal([]byte(call.Arguments), &args)

    if !looksDangerous(args.Command) {
        return nil
    }

    // Suspend the turn on an interactive confirm — this renders as a
    // yes/no prompt in the TUI (InteractivePromptRequestedEvent) and Web UI.
    confirmed, err := p.host.Confirm(ctx,
        "Run potentially destructive command?",
        args.Command)
    if err != nil || !confirmed {
        return &pluginapi.EventResponse{
            BlockToolExecution: true,
            BlockReason:        "blocked by guardrail plugin: user did not confirm",
        }
    }
    return nil
}
```

`BlockToolExecution`/`BlockReason` are only meaningful from a
`before_tool_exec` handler — see [Event → Response Field
Compatibility](/plugins#event-→-response-field-compatibility) for the full
table of which `EventResponse` fields apply to which events.

## Shaping every LLM call

`before_llm_call` is the highest-leverage event — it fires right before the
HTTP request goes out, with the full message list, model ID, and headers
available to inspect or rewrite:

```go
func (p *GuardrailPlugin) onBeforeLLMCall(call *pluginapi.BeforeLLMCallPayload) *pluginapi.EventResponse {
    return &pluginapi.EventResponse{
        InjectSystemPrompt: "Never suggest destroying data without an explicit, separate confirmation step.",
        AddHeaders: map[string]string{
            "X-Guardrail-Session": p.sessionTraceID,
        },
    }
}
```

`InjectSystemPrompt` is additive — it's appended to tau's existing system
prompt, not a replacement. Combine it with `InjectMessages` /
`RemoveMessageIndices` (valid on `"context"` and `"before_llm_call"`) if a
plugin needs to actively curate what the model sees, not just add to it.

## Notifying without blocking

`session_start` is a good place for non-blocking, informational feedback —
use `host.Notify()` rather than a confirm/input prompt so the session isn't
held up:

```go
func (p *GuardrailPlugin) onSessionStart(ctx context.Context, s *pluginapi.SessionEventPayload) *pluginapi.EventResponse {
    if p.host != nil {
        _ = p.host.Notify(ctx, "info", "guardrail plugin active for model "+s.ModelId)
    }
    return nil
}
```

`Notify()` pushes a transient toast to both the TUI (via the notify queue)
and the Web UI — it never blocks, unlike `Confirm`/`Input`.

## Putting it together

```go
type GuardrailPlugin struct {
    host           pluginapi.Host
    sessionTraceID string
}

func (p *GuardrailPlugin) SetHost(h pluginapi.Host) { p.host = h }

func (p *GuardrailPlugin) Metadata() (string, []*pluginapi.Command) {
    return "guardrail", nil // no slash commands
}

func (p *GuardrailPlugin) RunCommand(ctx context.Context, name, args string) (string, error) {
    return "", fmt.Errorf("guardrail plugin: no commands")
}

func (p *GuardrailPlugin) Tools(ctx context.Context) ([]*pluginapi.ToolDefinition, error) {
    return nil, nil // no tools
}

func (p *GuardrailPlugin) ExecuteTool(ctx context.Context, toolName, arguments string) (string, bool, error) {
    return "", true, fmt.Errorf("guardrail plugin: no tools")
}

func (p *GuardrailPlugin) Reload(ctx context.Context) ([]*pluginapi.Diagnostic, []*pluginapi.Command, error) {
    return nil, nil, nil
}
```

Wire it up with the same `plugin.Serve(...)` boilerplate shown in the [Quick
Start](/plugins#quick-start) — every plugin, simple or advanced, uses the
identical `main()`.

## Where this can go

- **Cost/rate guardrails**: track token usage from `after_llm_call`
  (`AfterLLMCallPayload.Usage`) and start rejecting turns via
  `before_tool_exec` once a budget is hit.
- **Compliance redaction**: scan `context`/`before_llm_call` messages and
  return `RemoveMessageIndices` for anything matching a secrets pattern
  before it reaches the model.
- **Human-in-the-loop tools**: pair `host.Input()` with `before_tool_exec` to
  ask the user to fill in a missing parameter rather than blocking outright.

All of these are combinations of the same three primitives covered here:
event hooks, `EventResponse`, and `Confirm`/`Input`/`Notify`. See the [full
event table](/plugins#event-table) for every event tau dispatches.
