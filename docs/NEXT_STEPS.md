# Tau Next Steps

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
  - discovery of project extensions under .tau/extensions or via .tau/config.[yml/yaml/json] (vcs-based repos or filesystem directories)
- Support user-created extensions for missing or local workflow features.
- Provide built-in, toggleable extensions for:
  - parallel agents/sub-agents
  - interactive tools such as questions, wizards, forms, and custom dialogs
- Expose lifecycle, runtime, tool, session, and TUI hook points.
- Support hot reload via an explicit `/reload` command. Filesystem watching is
  not required for the initial design.
- Keep built-in tools core and fast, while allowing extensions to wrap or
  override behavior where appropriate.

## Plugin Runtime Decision: go-plugin with gRPC

The chosen implementation for Tau's extension system is `hashicorp/go-plugin` using gRPC.

Reasons for this choice:

- **Language independence**: Plugins can be written in any language that supports gRPC (Go, Python, Rust, etc.).
- **Process isolation**: Plugins run in separate processes, protecting Tau from crashes.
- **Rich communication**: gRPC supports bidirectional streaming, perfect for LLM token streams and tool execution.
- **Proven architecture**: Used successfully by Terraform, Vault, and other industry-standard tools.

### Roadmap for Plugins

1. **Phase 1: Core Foundation** (Current)
   - Implement the `PluginManager` to discover and start binaries.
   - Define the initial gRPC service for metadata and lifecycle events.
   - Support tool registration via plugins.

2. **Phase 2: Expanded Hook Surface**
   - Add typed event payloads (OneOf).
   - Wire hooks for `turn_start`, `before_llm_call`, `after_tool_exec`, etc.
   - Support modifying internal state via plugin responses.

3. **Phase 3: Host Service API**
   - Allow plugins to call back into Tau.
   - APIs for session state, model discovery, and TUI notifications.
   - Persistent plugin-scoped configuration.

4. **Phase 4: Developer UX**
   - Plugin discovery via Git naming convention (`tau-plugin-*`).
   - `tau plugin new` scaffolder.
   - Official reference plugins (MCP client, planning agent).
