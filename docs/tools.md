# Tool System

Tau's tool system lets the LLM call functions during a turn — reading files, executing shell commands, searching code, and more. Tools are registered in a `Registry` and exposed to the LLM as function-calling schemas.

## Architecture

```
agent.Coordinator
    │
    ├── Registry.Schemas() → sent to LLM as tools[]
    │
    ├── LLM returns tool calls
    │
    ├── Registry.Get(name).Execute(params, uiBridge) → Result
    │
    └── executeToolsParallel() or sequential execution
```

## Registry

The `Registry` (`internal/agent/tools/registry.go`) is a thread-safe map of tool name → `Tool`. Key types:

```go
type Tool struct {
    Schema  Schema
    Execute Executor
    Source  string  // "builtin" or "plugin:<name>"
}

type Schema struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Parameters  json.RawMessage `json:"parameters"` // JSON Schema
}

type Executor func(ctx context.Context, params json.RawMessage, ui UIBridge) (Result, error)

type Result struct {
    Content string `json:"content"`
    Details any    `json:"details,omitempty"`
    IsError bool   `json:"is_error,omitempty"`
}
```

### Registry Methods

| Method | Description |
| ------ | ----------- |
| `Register(Tool) error` | Add a tool (returns error on duplicate) |
| `Replace(Tool) error` | Add or override a tool |
| `Unregister(name)` | Remove a tool |
| `Get(name) (Tool, bool)` | Look up a tool by name |
| `All() []Tool` | All tools in insertion order |
| `Schemas() []Schema` | All tool schemas (sent to LLM) |
| `Names() []string` | All tool names |
| `Count() int` | Number of registered tools |
| `RegisterPluginTool(pluginName, def) error` | Register a plugin tool with prefix |
| `UnregisterPluginTools(pluginName)` | Remove all tools from a plugin |
| `SetPluginToolExecutor(executor)` | Set the executor for plugin tools |

## Built-in Tools

Registered via `RegisterBuiltins()` in `internal/agent/tools/builtin.go`:

| Tool | File | Description |
| ---- | ---- | ----------- |
| `read` | `read.go` | Read file contents with line-range support |
| `write` | `write.go` | Create or overwrite files (queued in MutationQueue) |
| `edit` | `edit.go` | Precise text replacements in files (queued) |
| `shell` | `shell.go` | Execute shell commands with timeout |
| `grep` | `grep.go` | Search file contents with regex (regex by default; `literal: true` for plain text) |
| `find` | `find.go` | Find files by glob pattern or list a directory |
| `docs` | `docs.go` | Search, read, or list tau's embedded documentation |

The set is deliberately small: each tool has one clear job and no two tools overlap, so models pick the right one without deliberating. Session analysis showed that redundant tools (`patch`, `glob`, `ls`, split doc tools) went unused or pushed models to shell out instead.

## MutationQueue

Write and edit operations share a `MutationQueue` (`internal/agent/tools/mutation.go`). This enforces sequential execution of file mutations to prevent race conditions when tools run in parallel.

The queue:
- Serializes all write/edit operations
- Returns results in order
- Prevents interleaved writes to the same file

## UIBridge

Tools that need user interaction use `UIBridge`:

```go
type UIBridge interface {
    Confirm(ctx context.Context, title, description string) (bool, error)
    Select(ctx context.Context, title string, options []string) (string, error)
    Input(ctx context.Context, title, placeholder string) (string, error)
    Notify(title, level string)
}
```

The bridge implementation (`internal/agent/ui_bridge.go`) translates these calls into `InteractivePromptRequestedEvent` on the event bus. The TUI renders the prompt inline; the Web UI shows a dialog. The user's response comes back as `RespondInteractivePromptCommand`.

## Path Utilities

`internal/agent/tools/pathutil.go` provides safe path resolution:
- All paths are resolved relative to the configured working directory
- Path traversal (`../`) outside the working directory is blocked
- Home directory expansion (`~/`) is supported

## Content Truncation

`internal/agent/tools/truncate.go` provides content size management:
- Tool output is truncated to 2000 lines or 50KB, whichever is hit first
- `read` truncation appends an actionable continuation notice ("Use offset=N to continue") so the model can page through large files
- `shell` saves the full untruncated output to a temp file and includes the path in the notice, letting the model grep/tail it instead of re-running the command
- `grep` additionally caps output at 100 matches (adjustable via `limit`) and truncates individual lines to 500 chars so minified/generated files cannot blow out the context window

## Filesystem Utilities

`internal/agent/tools/fsutil.go` provides shared filesystem operations:
- Safe file reading with size limits
- Directory walking with pattern filtering
- File existence and permission checks

## Adding a Custom Tool

Custom tools are typically added via plugins (see [Plugin SDK](plugins.md)). For built-in tools:

1. Create a `Tool` with a JSON Schema and `Executor`.
2. Call `registry.Register(tool)` in `RegisterBuiltins()`.
3. The tool's name is what the LLM sees and uses in function calls.

Plugin tools are registered as `plugin:<name>:<tool>`. The `Registry.RegisterPluginTool()` method handles the prefixing and executor delegation.
