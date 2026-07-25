package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/pkg/plugin/api"
	"google.golang.org/grpc"
)

// Config configures the plugin manager.
type Config struct {
	PluginsDir           string          // directory containing plugin binaries, e.g. <config dir>/plugins
	ToolRegistry         *tools.Registry // tool registry for registering plugin tools
	Logger               *slog.Logger
	EventDispatchTimeout time.Duration // per-plugin event dispatch timeout (0 = default)
	ToolExecutionTimeout time.Duration // per-plugin tool execution timeout (0 = default)
	MaxViewsPerPlugin    int           // max concurrent open views per plugin (0 = default)

	// Plugins holds the `plugins.<name>` config blocks from config.yaml, served
	// to plugins via HostService.GetConfig.
	Plugins map[string]map[string]any
	// StateDir is where plugin SetConfig values persist (default: PluginsDir/..).
	StateDir string
	// LogOutput is where go-plugin's own lifecycle logging goes. It must never
	// be os.Stderr in interactive mode: stderr is the terminal the TUI draws
	// on, so handshake and shutdown noise would scroll over the UI. Defaults
	// to io.Discard; the app layer points it at the tau log file.
	LogOutput io.Writer
	// Optional host capabilities exposed to plugins via HostService.
	Notify       Notifier
	Models       func() []string
	SessionState func(sessionID string) (stateJSON string, found bool)
}

const (
	// DefaultEventDispatchTimeout is the maximum duration a single plugin may
	// take to respond to a lifecycle event dispatch. Event handlers are expected
	// to be fast (inspecting data only); 10s is generous for any plugin.
	DefaultEventDispatchTimeout = 10 * time.Second

	// DefaultToolExecutionTimeout is the maximum duration a single plugin tool
	// execution may take. Tools may perform real work (I/O, network calls).
	DefaultToolExecutionTimeout = 30 * time.Second

	// DefaultMaxViewsPerPlugin bounds how many distinct panels a single plugin
	// may have open at once, guarding against a misbehaving plugin flooding
	// the TUI. Updating an already-open view's content never counts against
	// this limit.
	DefaultMaxViewsPerPlugin = 5

	// maxParallelEventDispatch bounds how many plugins receive a lifecycle
	// event concurrently. Dispatch is still per-plugin timeout-bounded; this
	// just caps the number of in-flight RPCs so a large plugin set doesn't
	// open unbounded concurrent connections.
	maxParallelEventDispatch = 8
)

// pluginProcess is the subset of *goplugin.Client behavior the manager
// depends on for lifecycle management. It exists so tests can inject fake
// processes without spawning real plugin binaries.
type pluginProcess interface {
	Kill()
}

// pluginEntry is one loaded plugin's immutable-once-published state. Entries
// are never mutated after being placed in a registrySnapshot; a reload
// publishes a new entry (and a new snapshot) rather than editing this one in
// place, so concurrent readers never observe a half-updated plugin.
type pluginEntry struct {
	name         string
	process      pluginProcess
	grpc         *api.GRPCClient
	capabilities []string
	docs         string
}

// hasCapability reports whether the plugin advertises a capability. Plugins
// that advertise nothing (e.g. a legacy binary without the RPC) are treated
// as fully capable for backward compatibility.
func (pe *pluginEntry) hasCapability(capability string) bool {
	if len(pe.capabilities) == 0 {
		return true
	}
	return slices.Contains(pe.capabilities, capability)
}

// registrySnapshot is the manager's published view of loaded plugins. It is
// swapped atomically (never mutated in place) so readers never need a lock:
// they load the pointer once and see a fully-formed, self-consistent set of
// plugins for the lifetime of their call.
type registrySnapshot struct {
	entries map[string]*pluginEntry
	order   []string // load order, deterministic iteration
}

func emptySnapshot() *registrySnapshot {
	return &registrySnapshot{entries: make(map[string]*pluginEntry)}
}

// Manager discovers, launches, and manages go-plugin extension binaries.
// It implements chat.ExtensionReloader so it can be passed directly to the coordinator.
//
// Concurrency model: the published registry is an atomic snapshot pointer.
// Read paths (DispatchEvent, ExecutePluginTool, RunExtensionCommand,
// ExtensionCommands, PluginDocs) load the pointer once, without ever
// blocking on a mutex, and make plugin RPCs against the entries it names.
// Load and Unload never hold a lock while starting/killing a process or
// making a plugin RPC either: they build a candidate snapshot (or, for
// Unload, simply swap in an empty one) and publish it with a single atomic
// operation. Load additionally guards its publish with a compare-and-swap
// against the snapshot it started from, so a Load that raced a concurrent
// Load/Unload notices it lost, and retires (kills, unregisters tools for)
// the plugins it just started instead of silently leaking them or clobbering
// the winner's state.
type Manager struct {
	cfg  Config
	host *hostService // shared HostService served to plugins

	snapshot atomic.Pointer[registrySnapshot]

	// spawnPlugin starts a plugin binary and returns its process handle and
	// gRPC client. It is a field (not a direct call to startPlugin) so tests
	// can inject a fake without spawning real processes.
	spawnPlugin func(ctx context.Context, pluginPath string) (pluginProcess, *api.GRPCClient, error)
}

var _ chat.ExtensionReloader = &Manager{}

// NewManager creates a plugin manager that scans the plugins directory for
// extension binaries.
func NewManager(cfg Config) (*Manager, error) {
	if cfg.PluginsDir == "" {
		// Derive from the config dir so TAU_CONFIG_DIR is honoured; the
		// hot-reload sentinel path already resolves that way.
		cfg.PluginsDir = filepath.Join(tauconfig.Dir(), "plugins")
	}
	if cfg.LogOutput == nil {
		cfg.LogOutput = io.Discard
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.EventDispatchTimeout <= 0 {
		cfg.EventDispatchTimeout = DefaultEventDispatchTimeout
	}
	if cfg.ToolExecutionTimeout <= 0 {
		cfg.ToolExecutionTimeout = DefaultToolExecutionTimeout
	}
	if cfg.MaxViewsPerPlugin <= 0 {
		cfg.MaxViewsPerPlugin = DefaultMaxViewsPerPlugin
	}
	if cfg.StateDir == "" {
		cfg.StateDir = filepath.Dir(cfg.PluginsDir)
	}
	host := &hostService{
		logger:            cfg.Logger,
		config:            cfg.Plugins,
		kv:                newKVStore(filepath.Join(cfg.StateDir, "plugin-state.json")),
		notify:            cfg.Notify,
		models:            cfg.Models,
		sessionState:      cfg.SessionState,
		views:             make(map[string]map[string]struct{}),
		maxViewsPerPlugin: cfg.MaxViewsPerPlugin,
	}
	m := &Manager{
		cfg:  cfg,
		host: host,
	}
	m.spawnPlugin = func(ctx context.Context, pluginPath string) (pluginProcess, *api.GRPCClient, error) {
		return m.startPlugin(ctx, pluginPath)
	}
	return m, nil
}

// SetInteractiveHandler sets the interactive prompt handler on the shared host
// service so plugins can call Confirm/Input via the HostService RPCs.
func (m *Manager) SetInteractiveHandler(h InteractiveHandler) {
	m.host.interactivePrompt = h
}

// SetViewRenderer sets the panel renderer on the shared host service so
// plugins can call RenderView/CloseView via the HostService RPCs.
func (m *Manager) SetViewRenderer(r ViewRenderer) {
	m.host.viewRenderer = r
}

// Load discovers and starts all plugin binaries in the plugins directory,
// publishing them into the registry. No manager lock is held while starting
// processes or making plugin RPCs; only the final publish is synchronized,
// via a compare-and-swap against the snapshot Load started from.
func (m *Manager) Load(ctx context.Context) error {
	dirEntries, err := os.ReadDir(m.cfg.PluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("plugin manager: read plugins dir: %w", err)
	}

	base := m.snapshot.Load()
	baseSnap := base
	if baseSnap == nil {
		baseSnap = emptySnapshot()
	}

	next := &registrySnapshot{
		entries: maps.Clone(baseSnap.entries),
		order:   slices.Clone(baseSnap.order),
	}
	started := make(map[string]*pluginEntry)

	for _, entry := range dirEntries {
		if entry.IsDir() {
			continue
		}
		pluginPath := filepath.Join(m.cfg.PluginsDir, entry.Name())
		if !isExecutable(pluginPath) {
			continue
		}

		m.cfg.Logger.Debug("plugin manager: loading plugin", "path", pluginPath)

		name := entry.Name()
		if ext := filepath.Ext(name); ext != "" {
			name = name[:len(name)-len(ext)]
		}

		process, grpcClient, err := m.spawnPlugin(ctx, pluginPath)
		if err != nil {
			m.cfg.Logger.Warn("plugin manager: failed to start plugin", "path", pluginPath, "err", err)
			// A failed start leaves the plugin's tools missing for the whole
			// session, so tell the user rather than only the log file.
			if m.cfg.Notify != nil {
				m.cfg.Notify("warn", startFailureHint(name, err))
			}
			continue
		}

		pe := &pluginEntry{name: name, process: process, grpc: grpcClient}

		// Discover advertised capabilities so we skip unsupported calls.
		if caps, err := grpcClient.Capabilities(ctx); err == nil {
			pe.capabilities = caps
		}

		// Discover self-declared documentation (Documented interface), if any.
		if docs := grpcClient.Docs(ctx); docs != "" {
			pe.docs = docs
		}

		// Discover and register tools only if the plugin provides them.
		toolCount := 0
		if pe.hasCapability(api.CapabilityTools) {
			toolCount = m.registerPluginTools(ctx, name, grpcClient)
		}
		m.cfg.Logger.Info(
			"plugin manager: loaded plugin",
			"name", name,
			"commands", len(grpcClient.ExtensionCommands()),
			"tools", toolCount,
		)

		if _, exists := next.entries[name]; !exists {
			next.order = append(next.order, name)
		}
		next.entries[name] = pe
		started[name] = pe
	}

	if !m.snapshot.CompareAndSwap(base, next) {
		// A concurrent Load or Unload published a different snapshot while
		// we were starting plugins. Our newly started processes and
		// registered tools are orphaned against the now-current registry:
		// retire them explicitly instead of leaking the processes or
		// silently clobbering whichever snapshot won.
		m.retire(started)
		return fmt.Errorf("plugin manager: registry changed during load, retry")
	}

	return nil
}

// startFailureHint turns a plugin start error into a message worth showing a
// user. go-plugin reports a handshake protocol mismatch as an opaque
// "incompatible API version" error; the only remedy is rebuilding the plugin
// against the current pkg/plugin/api, so say that outright.
func startFailureHint(name string, err error) string {
	if strings.Contains(err.Error(), "incompatible API version") {
		return fmt.Sprintf(
			"plugin %s was built against an older tau plugin API and did not load - rebuild it against this tau version",
			name,
		)
	}
	return fmt.Sprintf("plugin %s failed to load: %v", name, err)
}

// retire kills each started plugin's process and unregisters any tools it
// registered. Used when a Load loses a race against a concurrent
// Load/Unload and must undo the work it just did.
func (m *Manager) retire(entries map[string]*pluginEntry) {
	for name, pe := range entries {
		if m.cfg.ToolRegistry != nil {
			m.cfg.ToolRegistry.UnregisterPluginTools(name)
		}
		m.cfg.Logger.Debug("plugin manager: retiring orphaned plugin load", "name", name)
		pe.process.Kill()
	}
}

// ReloadExtensions implements chat.ExtensionReloader.
func (m *Manager) ReloadExtensions(ctx context.Context, idle bool) (chat.ExtensionReloadResult, error) {
	m.Unload(ctx)
	if err := m.Load(ctx); err != nil {
		return chat.ExtensionReloadResult{}, err
	}

	snap := m.snapshot.Load()
	var result chat.ExtensionReloadResult
	if snap != nil {
		for _, name := range snap.order {
			pe := snap.entries[name]
			if pe == nil {
				continue
			}
			cmds := pe.grpc.ExtensionCommands()
			result.ExtensionCount++
			result.Commands = append(result.Commands, cmds...)
		}
	}
	return result, nil
}

// ExtensionCommands implements chat.ExtensionReloader.
func (m *Manager) ExtensionCommands() []chat.ExtensionCommand {
	snap := m.snapshot.Load()
	if snap == nil {
		return nil
	}

	var all []chat.ExtensionCommand
	for _, name := range snap.order {
		pe := snap.entries[name]
		if pe == nil {
			continue
		}
		all = append(all, pe.grpc.ExtensionCommands()...)
	}
	return all
}

// RunExtensionCommand implements chat.ExtensionReloader.
func (m *Manager) RunExtensionCommand(ctx context.Context, name, args string, uiBridge any) (string, *chat.ExtensionView, error) {
	snap := m.snapshot.Load()
	if snap != nil {
		for _, pname := range snap.order {
			pe := snap.entries[pname]
			if pe == nil {
				continue
			}
			for _, cmd := range pe.grpc.ExtensionCommands() {
				if commandHandles(cmd, name) {
					return pe.grpc.RunExtensionCommand(ctx, name, args, uiBridge)
				}
			}
		}
	}
	return "", nil, fmt.Errorf("plugin manager: command %q not found", name)
}

// commandHandles reports whether cmd owns the given command name. name is either
// the command itself ("mcp") or a space-joined sub-action path ("mcp list"); in
// the latter case the sub-action must be one the command declares.
func commandHandles(cmd chat.ExtensionCommand, name string) bool {
	if cmd.Name == name {
		return true
	}
	group, sub, ok := strings.Cut(name, " ")
	if !ok || group != cmd.Name {
		return false
	}
	for _, s := range cmd.Subcommands {
		if s.Name == sub {
			return true
		}
	}
	return false
}

// ExecutePluginTool routes a tool execution to the correct plugin.
func (m *Manager) ExecutePluginTool(ctx context.Context, pluginName, toolName string, args json.RawMessage) (tools.Result, error) {
	snap := m.snapshot.Load()
	var pe *pluginEntry
	if snap != nil {
		pe = snap.entries[pluginName]
	}
	if pe == nil {
		return tools.Result{IsError: true, Content: "plugin not found: " + pluginName}, nil
	}

	argsJSON := string(args)

	pluginCtx, cancel := context.WithTimeoutCause(ctx, m.cfg.ToolExecutionTimeout,
		fmt.Errorf("plugin %q timed out after %v executing tool %q", pluginName, m.cfg.ToolExecutionTimeout, toolName))
	defer cancel()

	resp, err := pe.grpc.Client.ExecuteTool(pluginCtx, &api.ExecuteToolRequest{
		ToolName:  toolName,
		Arguments: argsJSON,
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			m.cfg.Logger.Warn("plugin manager: tool execution timed out", "plugin", pluginName, "tool", toolName, "timeout", m.cfg.ToolExecutionTimeout)
		} else {
			m.cfg.Logger.Warn("plugin manager: tool execution failed", "plugin", pluginName, "tool", toolName, "err", err)
		}
		return tools.Result{IsError: true, Content: err.Error()}, nil
	}

	return tools.Result{
		Content: resp.GetContent(),
		IsError: resp.GetIsError(),
	}, nil
}

// DispatchEvent sends a lifecycle event with a typed payload to all plugins
// that advertise event support. Plugins are dispatched to concurrently
// (bounded by maxParallelEventDispatch), each under its own per-plugin
// timeout, so one hanging plugin cannot delay delivery to the rest. Despite
// running concurrently, responses are merged in a fixed, deterministic order
// (registry load order), independent of completion order.
// sessionID is the explicit session identity, passed from the coordinator rather
// than derived from the payload shape.
// Returns the merged EventResponse from all plugins.
func (m *Manager) DispatchEvent(ctx context.Context, event string, sessionID string, payload *api.EventPayload) *api.EventResponse {
	snap := m.snapshot.Load()
	if snap == nil {
		return nil
	}

	var targets []*pluginEntry
	for _, name := range snap.order {
		pe := snap.entries[name]
		if pe == nil || !pe.hasCapability(api.CapabilityEvents) {
			continue
		}
		targets = append(targets, pe)
	}

	m.cfg.Logger.Debug("plugin manager: dispatching event", "event", event, "session_id", sessionID, "plugin_count", len(targets))

	results := make([]*api.EventResponse, len(targets))

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxParallelEventDispatch)
	for i, pe := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, pe *pluginEntry) {
			defer wg.Done()
			defer func() { <-sem }()

			pluginCtx, cancel := context.WithTimeoutCause(ctx, m.cfg.EventDispatchTimeout,
				fmt.Errorf("plugin %q timed out after %v processing event %q", pe.name, m.cfg.EventDispatchTimeout, event))
			defer cancel()

			resp, err := pe.grpc.Client.DispatchEvent(pluginCtx, &api.DispatchEventRequest{
				Event:     event,
				SessionId: sessionID,
				Payload:   payload,
			})
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					m.cfg.Logger.Warn("plugin manager: dispatch timed out", "event", event, "plugin", pe.name, "timeout", m.cfg.EventDispatchTimeout)
				} else {
					m.cfg.Logger.Warn("plugin manager: dispatch failed", "event", event, "plugin", pe.name, "err", err)
				}
				return
			}
			if resp.GetResponse() != nil {
				m.cfg.Logger.Debug("plugin manager: got response", "event", event, "plugin", pe.name, "has_add_headers", len(resp.GetResponse().GetAddHeaders()) > 0)
				results[i] = resp.GetResponse()
			} else {
				m.cfg.Logger.Debug("plugin manager: nil response", "event", event, "plugin", pe.name)
			}
		}(i, pe)
	}
	wg.Wait()

	var responses []*api.EventResponse
	for _, r := range results {
		if r != nil {
			responses = append(responses, r)
		}
	}
	return mergeResponses(responses)
}

func mergeResponses(responses []*api.EventResponse) *api.EventResponse {
	if len(responses) == 0 {
		return nil
	}
	merged := &api.EventResponse{}
	for _, r := range responses {
		merged.InjectMessages = append(merged.GetInjectMessages(), r.GetInjectMessages()...)
		merged.RemoveMessageIndices = append(merged.GetRemoveMessageIndices(), r.GetRemoveMessageIndices()...)
		if r.GetInjectSystemPrompt() != "" {
			if merged.GetInjectSystemPrompt() != "" {
				merged.InjectSystemPrompt += "\n"
			}
			merged.InjectSystemPrompt += r.GetInjectSystemPrompt()
		}
		if r.GetBlockToolExecution() && !merged.GetBlockToolExecution() {
			merged.BlockToolExecution = true
			merged.BlockReason = r.GetBlockReason()
		}
		modifiedToolArgs := r.GetModifiedToolArguments()
		if modifiedToolArgs != "" {
			merged.ModifiedToolArguments = modifiedToolArgs
		}
		modifiedToolResult := r.GetModifiedToolResult()
		if modifiedToolResult != "" {
			merged.ModifiedToolResult = modifiedToolResult
		}
		if r.GetAddHeaders() != nil {
			if merged.GetAddHeaders() == nil {
				merged.AddHeaders = make(map[string]string)
			}
			maps.Copy(merged.GetAddHeaders(), r.GetAddHeaders())
		}
		if r.GetModifiedModelId() != "" {
			merged.ModifiedModelId = r.GetModifiedModelId()
		}
		merged.Diagnostics = append(merged.GetDiagnostics(), r.GetDiagnostics()...)
		if r.GetSuppressDefault() {
			merged.SuppressDefault = true
		}
	}
	return merged
}

// Unload stops all plugins and releases their resources. It publishes an
// empty registry snapshot with a single atomic swap before doing any of the
// slow cleanup (closing views, killing processes), so readers and dispatch
// calls never wait on a lock held across that work - they simply see the
// empty registry immediately. The swap unconditionally wins over a
// concurrent Load: a Load that started before this Unload committed will
// notice its compare-and-swap failed and retire what it started.
func (m *Manager) Unload(ctx context.Context) {
	old := m.snapshot.Swap(emptySnapshot())
	if old == nil {
		return
	}

	// Unregister all plugin tools and close any panels the plugin left open,
	// so a killed process never leaves stale UI state behind.
	for name := range old.entries {
		if m.cfg.ToolRegistry != nil {
			m.cfg.ToolRegistry.UnregisterPluginTools(name)
		}
		m.host.closeAllViewsForPlugin(ctx, name)
	}

	for name, pe := range old.entries {
		m.cfg.Logger.Debug("plugin manager: stopping plugin", "name", name)
		pe.process.Kill()
	}
}

// PluginDocs returns the self-declared documentation (via the optional
// Documented interface) of every currently loaded plugin, keyed by plugin
// name. Plugins that ship no documentation are omitted. Called on-demand by
// the docs tool so results always reflect the current plugin set, including
// after a hot reload (/mcp-reload, plugin install/uninstall).
func (m *Manager) PluginDocs() map[string]string {
	snap := m.snapshot.Load()
	if snap == nil || len(snap.entries) == 0 {
		return nil
	}
	out := make(map[string]string, len(snap.entries))
	for name, pe := range snap.entries {
		if pe.docs != "" {
			out[name] = pe.docs
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// registerPluginTools calls GetTools on the plugin and registers them in the
// tool registry as plugin-scoped tools.
func (m *Manager) registerPluginTools(ctx context.Context, pluginName string, c *api.GRPCClient) int {
	if m.cfg.ToolRegistry == nil {
		return 0
	}

	toolsResp, err := c.Client.GetTools(ctx, &api.GetToolsRequest{})
	if err != nil {
		m.cfg.Logger.Warn("plugin manager: failed to discover tools", "plugin", pluginName, "err", err)
		return 0
	}

	for _, td := range toolsResp.GetTools() {
		m.cfg.ToolRegistry.RegisterPluginTool(pluginName, tools.PluginToolDef{
			Name:        td.GetName(),
			Description: td.GetDescription(),
			InputSchema: td.GetInputSchema(),
		})
	}

	return len(toolsResp.GetTools())
}

func (m *Manager) startPlugin(ctx context.Context, pluginPath string) (*goplugin.Client, *api.GRPCClient, error) {
	logger := hclog.New(&hclog.LoggerOptions{
		Level:      hclog.Warn,
		Output:     m.cfg.LogOutput,
		JSONFormat: false,
		Name:       filepath.Base(pluginPath),
	})

	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig: api.Handshake,
		Plugins: map[string]goplugin.Plugin{
			"extension": &api.ExtensionPlugin{Impl: nil, HostImpl: m.host},
		},
		Cmd:              exec.CommandContext(ctx, pluginPath),
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		Logger:           logger,
		GRPCDialOptions:  []grpc.DialOption{},
	})

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("plugin manager: rpc connect: %w", err)
	}

	raw, err := rpcClient.Dispense("extension")
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("plugin manager: dispense: %w", err)
	}

	grpcClient, ok := raw.(*api.GRPCClient)
	if !ok {
		client.Kill()
		return nil, nil, fmt.Errorf("plugin manager: unexpected type %T from dispense", raw)
	}

	// Hand the plugin its HostService broker id, scoped by its reported name so
	// HostService.GetConfig resolves the right `plugins.<name>` block.
	pluginName := filepath.Base(pluginPath)
	if meta, err := grpcClient.Client.GetMetadata(ctx, &api.GetMetadataRequest{}); err == nil && meta.GetName() != "" {
		pluginName = meta.GetName()
	}
	if err := grpcClient.Init(ctx, pluginName); err != nil {
		m.cfg.Logger.Warn("plugin host init failed", "plugin", pluginName, "err", err)
	}

	return client, grpcClient, nil
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}

	if isExecutableByPlatform(path, info) {
		return true
	}

	// Fallback: Unix permission bits.
	// On Windows, isExecutableByPlatform handles the check; the permission-bit
	// fallback may produce false positives there (Go often reports 0777 for
	// regular files on Windows), but any false-positive file would simply fail
	// at launch time.
	return info.Mode()&0o111 != 0
}
