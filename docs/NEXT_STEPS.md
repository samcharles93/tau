# Tau Next Steps

These notes recreate the useful planning content from the AIM/Tau planning
thread. Tau should continue as the Go single-binary codebase, while Pi remains
a reference architecture for agent loops, tools, extension ideas, and runtime
UX patterns.

## Distribution: GoReleaser + Artifactory

GoReleaser is the preferred path for distributing Tau binaries.

Near-term next steps:

- Keep a local smoke-test path for releases:
  - `goreleaser release --snapshot --clean`
- Keep the publish path for CI releases:
  - `goreleaser release --clean`
- Ensure CI provides Artifactory credentials via environment variables, not
  committed config:
  - `ARTIFACTORY_URL`
  - `ARTIFACTORY_USER`
  - `ARTIFACTORY_PASSWORD`
- Verify that produced artifacts are named for the Tau binary and match the
  target OS/architecture matrix expected by users.
- Document how developers can test a snapshot locally without publishing.

Do not add internal Artifactory URLs or credentials to repository docs.

## Pi Fork/Rebrand Decision

The thread considered reusing Pi directly and rebranding it to AIM/Tau. For now,
the decision is **not to fork or rebrand Pi**.

Reasons captured in the planning thread:

- Pi's Node/native dependency chain created practical Windows friction.
- The observed install path failed around `canvas`, certificate validation for
  prebuilt packages, and `node-gyp`/Visual Studio C++ toolchain detection.
- Corporate Node versions, Windows setup, native packages, and certificate
  configuration make a soft fork riskier than continuing with the current Go
  codebase.

Pi should still be treated as a strong reference architecture. In particular,
Tau should borrow concepts from Pi's agent loop, modular tools, extension
system, hot reload, session persistence, and system-prompt structure where they
fit a Go implementation.

## Direction: Continue the Go Single Binary

Tau should stay focused on the Go single-binary approach:

- Keep the install and runtime story simple.
- Avoid making Node, native npm packages, or a Visual Studio C++ toolchain part
  of the default setup.
- Use Go-native implementations for performance-critical built-in tools.
- Use embedded resources where they help ship good defaults without requiring a
  separate runtime.

## Extension System Goals

Tau should adopt Pi's modular extension ideas without copying Pi's TypeScript
runtime model.

Primary goals:

- Allow project/team-specific extensions.
- Support user-created extensions for missing or local workflow features.
- Provide built-in, toggleable extensions for:
  - parallel agents/sub-agents
  - interactive tools such as questions, wizards, forms, and custom dialogs
- Expose lifecycle, runtime, tool, session, and TUI hook points.
- Support hot reload via an explicit `/reload` command. Filesystem watching is
  not required for the initial design.
- Keep built-in tools core and fast, while allowing extensions to wrap or
  override behavior where appropriate.

Extension authors are expected to include Tau maintainers, other users of the
system, and potentially Tau itself when guided to create an extension for a
missing feature.

## Runtime Options and Tradeoffs

The thread discussed several runtime options. No final implementation should
pretend all questions are closed, but the current preference is clear.

### Higher-priority options

#### Lua via gopher-lua

Lua is a strong candidate:

- proven embedded-extension pattern
- small runtime
- practical hot reload by replacing each extension VM/state
- simple API surface for registering hooks, tools, commands, and UI calls
- reasonable per-extension isolation when each extension gets its own Lua state

The main tradeoff is familiarity: Lua may not be widely known by every user.
The thread explicitly did not treat that as a blocker.

#### External process protocol

An external process protocol remains useful:

- language-agnostic
- naturally isolated from the host
- compatible with small programs written in Go, Python, PowerShell, or other
  tools users already have

Tradeoffs:

- IPC overhead
- protocol design and versioning work
- more complicated state sharing

The thread viewed this as viable, but not necessarily the best first path if
Lua can cover the core extension use cases.

#### YAML + exec

Declarative YAML plus executable hooks could support simple extension cases:

- easy to author for straightforward command wrappers
- useful for registering a tool that shells out

Tradeoffs:

- limited expressiveness
- creates two mental models once richer scripted extensions are needed

#### Yaegi

Yaegi is appealing because extensions could be written in Go:

- familiar to Go contributors
- access to much of the Go standard library

Tradeoffs:

- incomplete Go coverage
- rough edges
- interpreted code may be able to destabilize the host

### Lower-priority or ruled-out options

- **Go plugins**: ruled out for now because they are platform-constrained,
  version-coupled, and not a good Windows distribution fit.
- **Wasm**: lower priority because the authoring and host-interop complexity is
  high for the intended extension audience.
- **Goja**: lower priority because JavaScript embedding is heavier and the
  missing async/await model is awkward for Pi-like extension patterns.
- **Starlark**: lower priority because it is more configuration-oriented and
  limited for tool execution and interactive UI workflows.

## Proposed Architecture

The extension architecture should be layered around a single agent runtime.
There should not be a long-term split between "chat" and "agentic chat"; the
agent coordinator decides whether a turn needs tools.

### Agent Coordinator

The coordinator becomes the main runtime path:

1. Build messages from system prompt, history, context, skills, and user input.
2. Call the model with OpenAI-style tool definitions.
3. If tool calls are returned, execute them and append results to history.
4. Loop until the model returns a final response with no tool calls.

Important coordinator behaviors:

- OpenAI-style tool calling is the initial target.
- Parallel tool execution should be supported from day one.
- File mutation needs a queue to prevent conflicting concurrent writes.
- Tool output truncation should be adopted, with fuller logs or expansion
  mechanisms deferred.
- Built-in tools should register at startup.

### Extension Manager

Responsibilities:

- Discover extensions from global config locations and config-declared entries.
- Load, unload, and reload extensions.
- Own lifecycle transitions such as session start and shutdown.
- Maintain the extension registry for hooks, commands, tools, and UI features.

Likely discovery/config inputs:

- `~/.config/tau/extensions/`
- project or config-declared extension paths in `config.yaml`
- built-in embedded extensions shipped with Tau

### Extension Host

Responsibilities:

- Run each extension in its own host context.
- Provide the injected Tau API surface.
- Dispatch blocking and fire-and-forget events.
- Keep one extension failure from taking down unrelated extensions where
  practical.

For Lua, this likely means one `*lua.LState` per extension, replaced on reload.

### Registry

The registry should be the single place where tools and extension-provided
capabilities are made visible to the coordinator.

It should track:

- tool name, description, JSON schema, executor, and source
- slash commands
- lifecycle hooks
- TUI/interactive capabilities
- whether a tool is built-in or extension-provided

Built-in tools should be native Go functions for reliability and performance.
Built-in Lua extensions should be examples and higher-level behaviors, not the
implementation of hot-path file and shell tools.

### Permissions and Sandboxing

The initial trust model can be similar to Pi: extensions run with the user's
permissions. Heavy sandboxing is not a primary requirement in the thread.

Practical controls are still needed:

- permission gates for dangerous tools
- blocking hooks such as `before_tool_call`
- clear extension source/enablement state
- avoid silently executing untrusted project extensions without an explicit
  trust/enable path

### Event Dispatch

Use a hybrid sync/async model based on event type.

Blocking events:

- `session_start`
- `session_shutdown`
- `before_agent_start`
- `before_tool_call`
- `after_tool_result`
- input or command interception events

Fire-and-forget events:

- `message_delta`
- `message_complete`
- `turn_start`
- `turn_end`
- model-selection or observation-only UI events

### API Surface

The extension API should be Pi-like but adapted to Tau:

- register a tool
- register a slash command
- subscribe to events
- access session state through the store package
- append or adjust system-prompt guidance at supported hook points
- call interactive UI primitives through a TUI bridge
- optionally execute external commands where allowed

Interactive tools mean user-facing UI primitives such as confirmations,
questions, selections, forms, wizards, and custom dialogs. They do **not** mean
the core file/shell tools; those belong to the coordinator's built-in toolset.

## Near-term Implementation Plan

Suggested order from the planning thread:

1. Build the tool registry and native built-in tools:
   - read
   - write
   - edit/diff editing
   - bash
   - grep/ripgrep
   - find
   - ls
   - path utilities
2. Build the agent coordinator:
   - turn loop
   - OpenAI-style tool calls
   - parallel tool dispatch
   - mutation queue
   - output truncation
3. Build the system prompt builder:
   - tools
   - context files
   - skills
   - extension-provided guidance
4. Wire the coordinator as the single runtime path for the TUI.
5. Add the extension manager and Lua host:
   - discovery
   - load/unload/reload
   - lifecycle events
   - injected Tau API
6. Add the TUI bridge for interactive tools.
7. Add extension event hooks around coordinator and TUI operations.
8. Add `/reload` for extensions.
9. Add built-in toggleable extensions:
   - parallel agents
   - interactive tools

## Open Questions

- Should Lua be the first implemented runtime, or should the first slice keep
  the runtime boundary narrow enough to support Lua and external processes?
- What is the exact trust/enable flow for project-local extensions?
- How should extension permissions be represented in config?
- What output truncation limits should Tau use initially?
- How should users expand or inspect truncated tool logs later?
- What is the exact UI bridge contract for confirmations, forms, selections,
  and custom dialogs?
- How much of the existing chat runtime should be absorbed by the coordinator
  versus retained as a streaming helper?
- How should built-in extensions be toggled and documented?
- Should built-in extension examples be embedded with `go:embed`, extracted on
  first run, or both?
