# 61. Plugin Discovery via Git Naming Convention

## Status: Design

### Motivation

Plugin binaries sitting in `~/.config/tau/plugins/` requires manual installation. For tau to have an ecosystem, plugins need to be discoverable and installable with minimal friction. The established pattern — used by kubectl, gh CLI, oh-my-zsh, and many others — is a naming convention: `tau-plugin-<name>`.

### Design

**Discovery**: Plugins are Git repositories matching the pattern `tau-plugin-<name>`. They can live anywhere (GitHub, GitLab, self-hosted Gitea). Tau doesn't need a central registry — it searches GitHub, or users specify a repo directly.

**Installation**:

```shell
tau plugins search plugin           # searches GitHub for "tau-plugin-plugin"
tau plugins install plugin           # git clone → go build → install to ~/.config/tau/plugins/
tau plugins install gh:user/plugin   # install from specific GitHub repo
tau plugins install https://gitlab.example.com/team/tau-plugin-plugin
tau plugins list                  # show installed plugins
tau plugins update plugin            # git pull → rebuild → replace
tau plugins uninstall plugin        # remove binary
```

**Repository structure**:

```shell
tau-plugin-plugin/
├── main.go          # plugin entry point (implements plugin.Extension)
├── go.mod           # module tau-plugin-plugin
├── go.sum
└── README.md
```

No complex scaffolding — just a single Go binary. The `tau plugins new plugin` command scaffolds this structure.

**How it works**:

1. `tau plugins install plugin` searches GitHub API for repos named `tau-plugin-plugin`
2. If exactly one match, clones to `~/.cache/tau/plugins/src/tau-plugin-plugin`
3. Runs `go build -o ~/.config/tau/plugins/tau-plugin-plugin .`
4. Reloads the plugin manager (new binary appears in plugins dir)
5. Plugin connects, registers tools/commands

**Updates**: `tau plugins update` does `git pull` in the cached source, rebuilds, replaces the binary. Plugin manager detects the change and restarts the plugin.

**Compatibility**: Plugin declares required tau version range in its `Metadata()`. Host checks before loading:

```go
func (p *Plugin) Metadata() (string, []*proto.Command) {
    return "plugin-plugin", nil,
        proto.RequiresTau(">=0.5.0") // host validates
}
```

**Private plugins**: `tau plugins install gh:my-org/private-plugin` works with `gh auth` token. Self-hosted GitLab with `GITLAB_TOKEN` env var.

### Why Not a Central Registry?

Central registries (VS Code marketplace, npm) require infrastructure, moderation, and trust. The Git naming convention is:

- **Decentralized** — no single point of failure or control
- **Already understood** — kubectl, gh, oh-my-zsh users know the pattern
- **Versioned** — Git tags = plugin versions
- **Self-hostable** — enterprises can run their own GitLab with private plugins
- **Zero infra cost** — GitHub search is free

If the ecosystem grows large enough to need curation, an optional index repo (`tau-plugins/index`) can provide a curated list with metadata, but it's additive, not required.

### Comparison: gh CLI Extension Architecture

The gh CLI's extension system (https://github.com/cli/cli/tree/trunk/pkg/extensions) is a model of simplicity:

```go
// The entire extension interface (1K lines of code total in the package):
type Extension interface {
    Name() string           // name without gh- prefix
    Path() string           // path to executable
    URL() string            // repo URL
    CurrentVersion() string
    LatestVersion() string
    IsPinned() bool
    UpdateAvailable() bool
    IsBinary() bool
    IsLocal() bool
    Owner() string
}

type ExtensionManager interface {
    List() []Extension
    Install(ghrepo.Interface, string) error
    InstallLocal(dir string) error
    Upgrade(name string, force bool) error
    Remove(name string) error
    Dispatch(args []string, stdin io.Reader, stdout, stderr io.Writer) (bool, error)
    Create(name string, tmplType ExtTemplateType) error
    EnableDryRunMode()
    UpdateDir(name string) string
}
```

**Key differences from tau's go-plugin approach**:

| Concern | gh CLI | tau (go-plugin) |
| ------- | ------ | --------------- |
| Communication | stdin/stdout | gRPC over stdio |
| Process model | One-shot exec per command | Long-lived process |
| Capabilities | Implicit (binary name = subcommand) | Explicit (proto service discovery) |
| Installation | `gh repo clone` + auto-detect on PATH | Binary in plugins dir |
| Interface size | ~10 methods (manager) | ~50+ methods (proto services) |
| Language support | Any language (just needs a binary) | Any gRPC-capable language |

**What tau should adopt from gh**:

- **Naming convention + PATH discovery** — `tau-plugin-*` binaries on PATH
- **Small manager interface** — the manager doesn't need to know about capabilities
- **Official extensions list** — a curated set of GitHub-owned extensions for discovery
- **`Dispatch` with stdin/stdout** — for simple command extensions, gRPC is overkill

**What tau gets from go-plugin that gh doesn't**:

- Rich bidirectional streaming (LLM tokens, pipeline processing)
- Tool registration with the agent
- Event hooks (session lifecycle, tool calls)
- Health checking and auto-restart
- Capability negotiation at handshake time

**The right model for tau**: Both. Simple command extensions use the gh model (binary on PATH, exec on demand). Rich capability plugins use go-plugin (long-lived process, gRPC streaming). A plugin declares its mode in its manifest — the manager routes accordingly.
