# 24. Migrate Extension Architecture from Lua to go-plugin with gRPC

## Status: Researching

### Motivation

The current extensions architecture runs scripts in-process. This has several limitations:

- **Crash risk**: A bug in an extension can crash the entire Tau process.
- **Language lock-in**: Extensions must be written in a specific language (Lua).
- **Security**: Difficult to sandbox in-process scripts.
- **Versioning**: No clean way to handle plugin versions and dependencies.

### Design: go-plugin + gRPC

Adopt Hashicorp's `go-plugin` architecture (used by Terraform, Vault, Nomad):

- Extensions are standalone binaries.
- Communication happens via gRPC over stdio.
- Language agnostic: any language that supports gRPC can be used.

**Phase 1: Go interface definition**

```go
// pkg/plugin/extension.go
type Extension interface {
    Metadata() (name string, commands []*Command)
    RunCommand(ctx context.Context, name, args string) (string, error)
    Reload(ctx context.Context) ([]*Diagnostic, []*Command, error)
    DispatchEvent(ctx context.Context, event string, context map[string]string)
}

type Command struct {
    Name        string
    Description string
    Handler     func(ctx context.Context, args string) (string, error)
}

type SessionInfo struct {
    ID        string
    ModelID   string
    Provider  string
    CreatedAt time.Time
}
```

**Phase 2: Proto service definition**

```protobuf
// internal/plugin/proto/extension.proto
service ExtensionService {
  rpc GetName(GetName.Request) returns (GetName.Response);
  rpc GetCommands(GetCommands.Request) returns (GetCommands.Response);
  rpc OnSessionStart(SessionEvent) returns (google.protobuf.Empty);
  rpc OnSessionShutdown(SessionEvent) returns (google.protobuf.Empty);
  rpc StreamChat(stream StreamChat.Chunk) returns (stream StreamChat.Chunk);
}
```

**Phase 3: Plugin discovery and lifecycle**

- `~/.config/tau/plugins/` directory scanned on startup
- Each subdirectory contains a compiled plugin binary + optional config
- `tau plugins list` shows loaded plugins with status
- `tau plugins reload` sends SIGHUP to all plugins and re-discovers
- Plugin crashes are detected via health check; auto-restart with backoff

**Streaming benefit for Tau**: The `StreamChat` RPC uses bidirectional streaming — the host sends user prompts and receives LLM tokens, tool call intermediates, and final responses over the same gRPC stream. This is a direct replacement for the current coordinator's `StreamChatCompletionFull` callback pattern.

### Files to Create/Modify

| File | Action | Phase |
| ---- | ------ | ----- |
| `pkg/plugin/extension.go` | New — public `Extension` interface | 1 |
| `pkg/plugin/command.go` | New — command registration types | 1 |
| `pkg/plugin/events.go` | New — session/tool event types | 1 |
| `internal/plugin/proto/extension.proto` | New — protobuf service definition | 2 |
| `internal/plugin/proto/` | New — generated Go code (`protoc-gen-go`, `protoc-gen-go-grpc`) | 2 |
| `internal/plugin/server.go` | New — gRPC server adapter + ExtensionPlugin shim | 2 |
| `internal/plugin/client.go` | New — gRPC client adapter | 2 |
| `internal/plugin/manager.go` | New — plugin discovery, lifecycle, health checking | 3 |
| `internal/app/chat.go` | Modify — replace extensionManager with pluginManager | 3 |
| `go.mod` | Modify — add `google.golang.org/grpc`, `google.golang.org/protobuf`, `github.com/hashicorp/go-plugin` | 1 |

### Risks and Mitigations

| Risk | Severity | Mitigation |
| ---- | -------- | ---------- |
| Plugin binary compilation burden | Medium | Ship a `tau plugin new` scaffolder; provide pre-built plugin binaries via `tau plugins install` |
| gRPC + protobuf adds build complexity | Medium | `//go:generate` directives; CI pipeline handles proto generation |
| Plugin version mismatch | Low | Handshake protocol version check on connect; plugin manifest with semver range |
| Startup latency (spawning processes) | Low | Plugins started once and kept alive; health check pings are cheap |
| Memory overhead (N plugin processes) | Low | Typical deployment is 1-5 plugins; each Go plugin ~10-20MB RSS |
