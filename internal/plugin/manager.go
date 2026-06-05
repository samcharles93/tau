package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/pkg/plugin/api"
	"google.golang.org/grpc"
)

// Config configures the plugin manager.
type Config struct {
	PluginsDir   string          // directory containing plugin binaries, e.g. ~/.config/tau/plugins
	ToolRegistry *tools.Registry // tool registry for registering plugin tools
	Logger       *slog.Logger
}

// Manager discovers, launches, and manages go-plugin extension binaries.
// It implements chat.ExtensionReloader so it can be passed directly to the coordinator.
type Manager struct {
	cfg         Config
	clients     map[string]*goplugin.Client // plugin name → client
	grpcClients map[string]*api.GRPCClient  // plugin name → gRPC client
	pluginOrder []string                    // load order, deterministic iteration
	mu          sync.RWMutex
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
	return &Manager{
		cfg:         cfg,
		clients:     make(map[string]*goplugin.Client),
		grpcClients: make(map[string]*api.GRPCClient),
	}, nil
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

		// Discover and register tools.
		toolCount := m.registerPluginTools(ctx, name, grpcClient)
		m.cfg.Logger.Info("plugin manager: loaded plugin",
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
func (m *Manager) RunExtensionCommand(ctx context.Context, name, args string, uiBridge any) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, c := range m.grpcClients {
		for _, cmd := range c.ExtensionCommands() {
			if cmd.Name == name {
				return c.RunExtensionCommand(ctx, name, args, uiBridge)
			}
		}
	}
	return "", fmt.Errorf("plugin manager: command %q not found", name)
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
	resp, err := c.Client.ExecuteTool(ctx, &api.ExecuteToolRequest{
		ToolName:  toolName,
		Arguments: argsJSON,
	})
	if err != nil {
		return tools.Result{IsError: true, Content: err.Error()}, nil
	}

	return tools.Result{
		Content: resp.Content,
		IsError: resp.IsError,
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
		resp, err := c.Client.DispatchEvent(ctx, &api.DispatchEventRequest{
			Event:     event,
			SessionId: sessionID,
			Payload:   payload,
		})
		if err != nil {
			m.cfg.Logger.Warn("plugin manager: dispatch failed", "event", event, "plugin", name, "err", err)
			continue
		}
		if resp.Response != nil {
			m.cfg.Logger.Debug("plugin manager: got response", "event", event, "plugin", name, "has_add_headers", len(resp.Response.AddHeaders) > 0)
			responses = append(responses, resp.Response)
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
		merged.InjectMessages = append(merged.InjectMessages, r.InjectMessages...)
		merged.RemoveMessageIndices = append(merged.RemoveMessageIndices, r.RemoveMessageIndices...)
		if r.InjectSystemPrompt != "" {
			if merged.InjectSystemPrompt != "" {
				merged.InjectSystemPrompt += "\n"
			}
			merged.InjectSystemPrompt += r.InjectSystemPrompt
		}
		if r.BlockToolExecution && !merged.BlockToolExecution {
			merged.BlockToolExecution = true
			merged.BlockReason = r.BlockReason
		}
		modifiedToolArgs := r.ModifiedToolArguments
		if modifiedToolArgs != "" {
			merged.ModifiedToolArguments = modifiedToolArgs
		}
		modifiedToolResult := r.ModifiedToolResult
		if modifiedToolResult != "" {
			merged.ModifiedToolResult = modifiedToolResult
		}
		if r.AddHeaders != nil {
			if merged.AddHeaders == nil {
				merged.AddHeaders = make(map[string]string)
			}
			maps.Copy(merged.AddHeaders, r.AddHeaders)
		}
		if r.ModifiedModelId != "" {
			merged.ModifiedModelId = r.ModifiedModelId
		}
		merged.Diagnostics = append(merged.Diagnostics, r.Diagnostics...)
		if r.SuppressDefault {
			merged.SuppressDefault = true
		}
	}
	return merged
}

// Unload stops all plugins and releases their resources.
func (m *Manager) Unload() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Unregister all plugin tools.
	if m.cfg.ToolRegistry != nil {
		for name := range m.grpcClients {
			m.cfg.ToolRegistry.UnregisterPluginTools(name)
		}
	}

	for name, client := range m.clients {
		m.cfg.Logger.Debug("plugin manager: stopping plugin", "name", name)
		client.Kill()
	}
	m.clients = make(map[string]*goplugin.Client)
	m.grpcClients = make(map[string]*api.GRPCClient)
	m.pluginOrder = nil
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

	for _, td := range toolsResp.Tools {
		m.cfg.ToolRegistry.RegisterPluginTool(pluginName, tools.PluginToolDef{
			Name:        td.Name,
			Description: td.Description,
			InputSchema: td.InputSchema,
		})
	}

	return len(toolsResp.Tools)
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
			"extension": &api.ExtensionPlugin{Impl: nil},
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
