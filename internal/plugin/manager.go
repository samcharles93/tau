package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
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
)

// Manager discovers, launches, and manages go-plugin extension binaries.
// It implements chat.ExtensionReloader so it can be passed directly to the coordinator.
type Manager struct {
	cfg          Config
	host         *hostService                // shared HostService served to plugins
	clients      map[string]*goplugin.Client // plugin name → client
	grpcClients  map[string]*api.GRPCClient  // plugin name → gRPC client
	capabilities map[string][]string         // plugin name → advertised capabilities
	docs         map[string]string           // plugin name → self-declared docs (Documented)
	pluginOrder  []string                    // load order, deterministic iteration
	mu           sync.RWMutex
}

var _ chat.ExtensionReloader = &Manager{}

// NewManager creates a plugin manager that scans the plugins directory for
// extension binaries.
func NewManager(cfg Config) (*Manager, error) {
	if cfg.PluginsDir == "" {
		home, _ := os.UserHomeDir()
		cfg.PluginsDir = filepath.Join(home, ".config", "tau", "plugins")
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
	return &Manager{
		cfg:          cfg,
		host:         host,
		clients:      make(map[string]*goplugin.Client),
		grpcClients:  make(map[string]*api.GRPCClient),
		capabilities: make(map[string][]string),
		docs:         make(map[string]string),
	}, nil
}

// hasCapability reports whether the named plugin advertises a capability.
// Plugins that advertise nothing (e.g. a legacy binary without the RPC) are
// treated as fully capable for backward compatibility.
func (m *Manager) hasCapability(name, capability string) bool {
	caps, ok := m.capabilities[name]
	if !ok || len(caps) == 0 {
		return true
	}
	return slices.Contains(caps, capability)
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

// Load discovers and starts all plugin binaries in the plugins directory.
func (m *Manager) Load(ctx context.Context) error {
	entries, err := os.ReadDir(m.cfg.PluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("plugin manager: read plugins dir: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		pluginPath := filepath.Join(m.cfg.PluginsDir, entry.Name())
		if !isExecutable(pluginPath) {
			continue
		}

		m.cfg.Logger.Debug("plugin manager: loading plugin", "path", pluginPath)

		client, grpcClient, err := m.startPlugin(ctx, pluginPath)
		if err != nil {
			m.cfg.Logger.Warn("plugin manager: failed to start plugin", "path", pluginPath, "err", err)
			continue
		}

		name := entry.Name()
		if ext := filepath.Ext(name); ext != "" {
			name = name[:len(name)-len(ext)]
		}

		m.clients[name] = client
		m.grpcClients[name] = grpcClient
		m.pluginOrder = append(m.pluginOrder, name)

		// Discover advertised capabilities so we skip unsupported calls.
		if caps, err := grpcClient.Capabilities(ctx); err == nil {
			m.capabilities[name] = caps
		}

		// Discover self-declared documentation (Documented interface), if any.
		if docs := grpcClient.Docs(ctx); docs != "" {
			m.docs[name] = docs
		}

		// Discover and register tools only if the plugin provides them.
		toolCount := 0
		if m.hasCapability(name, api.CapabilityTools) {
			toolCount = m.registerPluginTools(ctx, name, grpcClient)
		}
		m.cfg.Logger.Info(
			"plugin manager: loaded plugin",
			"name", name,
			"commands", len(grpcClient.ExtensionCommands()),
			"tools", toolCount,
		)
	}

	return nil
}

// ReloadExtensions implements chat.ExtensionReloader.
func (m *Manager) ReloadExtensions(ctx context.Context, idle bool) (chat.ExtensionReloadResult, error) {
	m.Unload()
	if err := m.Load(ctx); err != nil {
		return chat.ExtensionReloadResult{}, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var result chat.ExtensionReloadResult
	for _, c := range m.grpcClients {
		cmds := c.ExtensionCommands()
		result.ExtensionCount++
		result.Commands = append(result.Commands, cmds...)
	}
	return result, nil
}

// ExtensionCommands implements chat.ExtensionReloader.
func (m *Manager) ExtensionCommands() []chat.ExtensionCommand {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var all []chat.ExtensionCommand
	for _, c := range m.grpcClients {
		all = append(all, c.ExtensionCommands()...)
	}
	return all
}

// RunExtensionCommand implements chat.ExtensionReloader.
func (m *Manager) RunExtensionCommand(ctx context.Context, name, args string, uiBridge any) (string, *chat.ExtensionView, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, c := range m.grpcClients {
		for _, cmd := range c.ExtensionCommands() {
			if commandHandles(cmd, name) {
				return c.RunExtensionCommand(ctx, name, args, uiBridge)
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
	m.mu.RLock()
	c, ok := m.grpcClients[pluginName]
	m.mu.RUnlock()

	if !ok {
		return tools.Result{IsError: true, Content: "plugin not found: " + pluginName}, nil
	}

	argsJSON := string(args)

	pluginCtx, cancel := context.WithTimeoutCause(ctx, m.cfg.ToolExecutionTimeout,
		fmt.Errorf("plugin %q timed out after %v executing tool %q", pluginName, m.cfg.ToolExecutionTimeout, toolName))
	defer cancel()

	resp, err := c.Client.ExecuteTool(pluginCtx, &api.ExecuteToolRequest{
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

// DispatchEvent sends a lifecycle event with a typed payload to all plugins.
// sessionID is the explicit session identity, passed from the coordinator rather
// than derived from the payload shape.
// Returns the merged EventResponse from all plugins.
func (m *Manager) DispatchEvent(ctx context.Context, event string, sessionID string, payload *api.EventPayload) *api.EventResponse {
	m.mu.RLock()
	defer m.mu.RUnlock()

	m.cfg.Logger.Debug("plugin manager: dispatching event", "event", event, "session_id", sessionID, "plugin_count", len(m.grpcClients))

	var responses []*api.EventResponse
	for _, name := range m.pluginOrder {
		c, ok := m.grpcClients[name]
		if !ok {
			continue
		}
		// Skip plugins that do not handle lifecycle events.
		if !m.hasCapability(name, api.CapabilityEvents) {
			continue
		}

		pluginCtx, cancel := context.WithTimeoutCause(ctx, m.cfg.EventDispatchTimeout,
			fmt.Errorf("plugin %q timed out after %v processing event %q", name, m.cfg.EventDispatchTimeout, event))
		resp, err := c.Client.DispatchEvent(pluginCtx, &api.DispatchEventRequest{
			Event:     event,
			SessionId: sessionID,
			Payload:   payload,
		})
		cancel()

		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				m.cfg.Logger.Warn("plugin manager: dispatch timed out", "event", event, "plugin", name, "timeout", m.cfg.EventDispatchTimeout)
			} else {
				m.cfg.Logger.Warn("plugin manager: dispatch failed", "event", event, "plugin", name, "err", err)
			}
			continue
		}
		if resp.GetResponse() != nil {
			m.cfg.Logger.Debug("plugin manager: got response", "event", event, "plugin", name, "has_add_headers", len(resp.GetResponse().GetAddHeaders()) > 0)
			responses = append(responses, resp.GetResponse())
		} else {
			m.cfg.Logger.Debug("plugin manager: nil response", "event", event, "plugin", name)
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

// Unload stops all plugins and releases their resources.
func (m *Manager) Unload() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Unregister all plugin tools and close any panels the plugin left open,
	// so a killed process never leaves stale UI state behind.
	for name := range m.grpcClients {
		if m.cfg.ToolRegistry != nil {
			m.cfg.ToolRegistry.UnregisterPluginTools(name)
		}
		m.host.closeAllViewsForPlugin(context.Background(), name)
	}

	for name, client := range m.clients {
		m.cfg.Logger.Debug("plugin manager: stopping plugin", "name", name)
		client.Kill()
	}
	m.clients = make(map[string]*goplugin.Client)
	m.grpcClients = make(map[string]*api.GRPCClient)
	m.capabilities = make(map[string][]string)
	m.docs = make(map[string]string)
	m.pluginOrder = nil
}

// PluginDocs returns the self-declared documentation (via the optional
// Documented interface) of every currently loaded plugin, keyed by plugin
// name. Plugins that ship no documentation are omitted. Called on-demand by
// the docs tool so results always reflect the current plugin set, including
// after a hot reload (/mcp-reload, plugin install/uninstall).
func (m *Manager) PluginDocs() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.docs) == 0 {
		return nil
	}
	out := make(map[string]string, len(m.docs))
	maps.Copy(out, m.docs)
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
		Output:     os.Stderr,
		JSONFormat: false,
		Name:       filepath.Base(pluginPath),
	})

	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig: goplugin.HandshakeConfig{
			ProtocolVersion:  1,
			MagicCookieKey:   "TAU_PLUGIN",
			MagicCookieValue: "tau",
		},
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
