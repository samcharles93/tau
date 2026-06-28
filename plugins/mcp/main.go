// Tau MCP Client Plugin — connects to MCP servers and registers their tools
// with tau's agent coordinator via the go-plugin extension architecture.
//
// Build: go build -o tau-plugin-mcp ./cmd/tau-plugin-mcp/
// Install: cp tau-plugin-mcp ~/.config/tau/plugins/
//
// Config (~/.config/tau/config.yaml):
//
//	plugins:
//	  mcp-plugin:
//	    servers:
//	      - name: filesystem
//	        command: npx
//	        args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
//	      - name: postgres
//	        command: npx
//	        args: ["-y", "@modelcontextprotocol/server-postgres", "$DATABASE_URL"]
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	pluginapi "github.com/samcharles93/tau/pkg/plugin/api"
)

// MCPPlugin implements pluginapi.Extension, connecting to configured MCP
// servers and bridging their tools into tau's agent.
type MCPPlugin struct {
	mu       sync.RWMutex
	sessions map[string]*mcpSession // server name → MCP session
	logger   *slog.Logger
	host     pluginapi.Host
}

// SetHost receives the host handle so MCP server config comes from tau's
// config subsystem (plugins.mcp-plugin.servers) rather than an env var.
func (p *MCPPlugin) SetHost(h pluginapi.Host) {
	p.host = h
}

// Capabilities advertises that this plugin provides tools, slash commands, and
// interactive prompts (Confirm/Input via HostService).
func (p *MCPPlugin) Capabilities() []string {
	return []string{pluginapi.CapabilityTools, pluginapi.CapabilityCommands, pluginapi.CapabilityInteractive}
}

type mcpSession struct {
	name    string
	session *mcp.ClientSession
	client  *mcp.Client
	cmd     *exec.Cmd // nil for non-stdio transports
}

func main() {
	logger := hclog.New(&hclog.LoggerOptions{
		Level:      hclog.Info,
		Output:     os.Stderr,
		JSONFormat: false,
		Name:       "tau-plugin-mcp",
	})

	p := &MCPPlugin{
		sessions: make(map[string]*mcpSession),
		logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: plugin.HandshakeConfig{
			ProtocolVersion:  1,
			MagicCookieKey:   "TAU_PLUGIN",
			MagicCookieValue: "tau",
		},
		Plugins: map[string]plugin.Plugin{
			"extension": &pluginapi.ExtensionPlugin{Impl: p},
		},
		GRPCServer: plugin.DefaultGRPCServer,
		Logger:     logger,
	})
}

// Metadata returns the plugin name and slash commands.
func (p *MCPPlugin) Metadata() (string, []*pluginapi.Command) {
	return "mcp-plugin", []*pluginapi.Command{
		{Name: "mcp-list", Description: "list connected MCP servers", ExtensionName: "mcp-plugin"},
		{Name: "mcp-reconnect", Description: "reconnect to an MCP server by name", ExtensionName: "mcp-plugin"},
		{Name: "mcp-reload", Description: "reload all MCP servers from config", ExtensionName: "mcp-plugin"},
	}
}

// RunCommand executes a slash command.
func (p *MCPPlugin) RunCommand(ctx context.Context, name, args string) (string, error) {
	switch name {
	case "mcp-list":
		return p.cmdList()
	case "mcp-reconnect":
		return p.cmdReconnect(ctx, strings.TrimSpace(args))
	case "mcp-reload":
		return p.cmdReload(ctx)
	default:
		return "", fmt.Errorf("tau-plugin-mcp: unknown command %q", name)
	}
}

// Reload reconnects to MCP servers.
func (p *MCPPlugin) Reload(ctx context.Context) ([]*pluginapi.Diagnostic, []*pluginapi.Command, error) {
	p.mu.Lock()
	// Close existing sessions.
	for name, s := range p.sessions {
		s.session.Close()
		if s.cmd != nil && s.cmd.Process != nil {
			s.cmd.Process.Kill()
		}
		delete(p.sessions, name)
	}
	p.mu.Unlock()

	if err := p.loadServers(ctx); err != nil {
		return []*pluginapi.Diagnostic{{
			Severity: "error",
			Message:  err.Error(),
		}}, nil, nil
	}
	_, cmds := p.Metadata()
	return nil, cmds, nil
}

// DispatchEvent handles lifecycle events.
func (p *MCPPlugin) DispatchEvent(ctx context.Context, event string, sessionID string, payload *pluginapi.EventPayload) *pluginapi.EventResponse {
	p.logger.Debug("tau-plugin-mcp: event", "event", event, "session_id", sessionID)
	return nil
}

// Tools returns all tools from all connected MCP servers.
func (p *MCPPlugin) Tools(ctx context.Context) ([]*pluginapi.ToolDefinition, error) {
	p.mu.RLock()
	needsLoad := len(p.sessions) == 0
	p.mu.RUnlock()

	if needsLoad {
		// Lazy-load servers on first Tools call (release lock first to avoid deadlock).
		_ = p.loadServers(ctx)
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	var tools []*pluginapi.ToolDefinition
	for _, s := range p.sessions {
		serverTools, err := s.listTools(ctx)
		if err != nil {
			p.logger.Warn("tau-plugin-mcp: failed to list tools", "server", s.name, "err", err)
			continue
		}
		tools = append(tools, serverTools...)
	}
	return tools, nil
}

// ExecuteTool executes an MCP tool and returns the result.
func (p *MCPPlugin) ExecuteTool(ctx context.Context, toolName, arguments string) (content string, isError bool, err error) {
	serverName, shortName, ok := strings.Cut(toolName, ".")
	if !ok {
		return "", true, fmt.Errorf("tau-plugin-mcp: invalid tool name %q (expected server.tool)", toolName)
	}

	p.mu.RLock()
	session, ok := p.sessions[serverName]
	p.mu.RUnlock()

	if !ok {
		// Try lazy-loading if not yet connected.
		_ = p.loadServers(ctx)
		p.mu.RLock()
		session, ok = p.sessions[serverName]
		p.mu.RUnlock()
		if !ok {
			return "", true, fmt.Errorf("tau-plugin-mcp: server %q not connected", serverName)
		}
	}

	return session.callTool(ctx, shortName, arguments)
}

// --- Slash command handlers ---

func (p *MCPPlugin) cmdList() (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.sessions) == 0 {
		return "No MCP servers connected.", nil
	}

	var b strings.Builder
	b.WriteString("Connected MCP servers:\n")
	for name, s := range p.sessions {
		tools, err := s.listTools(context.Background())
		toolCount := 0
		if err == nil {
			toolCount = len(tools)
		}
		fmt.Fprintf(&b, "  %-20s  %d tools\n", name, toolCount)
	}
	return b.String(), nil
}

func (p *MCPPlugin) cmdReconnect(ctx context.Context, serverName string) (string, error) {
	if serverName == "" {
		return "", fmt.Errorf("usage: /mcp-reconnect <server>")
	}

	// Confirm before disconnecting.
	if p.host != nil {
		confirmed, err := p.host.Confirm(ctx,
			"Reconnect MCP server",
			"Disconnect and reconnect to "+serverName+"? This will interrupt any active connections.")
		if err != nil || !confirmed {
			if err != nil {
				return "", fmt.Errorf("confirmation failed: %w", err)
			}
			return "Cancelled.", nil
		}
	}

	p.mu.Lock()
	s, ok := p.sessions[serverName]
	if !ok {
		p.mu.Unlock()
		return "", fmt.Errorf("server %q not found — use /mcp-list to see connected servers", serverName)
	}
	s.session.Close()
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
	delete(p.sessions, serverName)
	p.mu.Unlock()

	servers, err := p.readConfig()
	if err != nil {
		return "", fmt.Errorf("read config: %w", err)
	}

	for _, cfg := range servers {
		if cfg.Name == serverName && cfg.Command != "" {
			if err := p.connectServer(context.Background(), cfg); err != nil {
				return "", fmt.Errorf("reconnect failed: %w", err)
			}
			return fmt.Sprintf("✓ reconnected to %s", serverName), nil
		}
	}
	return "", fmt.Errorf("server %q not found in config", serverName)
}

func (p *MCPPlugin) cmdReload(ctx context.Context) (string, error) {
	if p.host != nil {
		_ = p.host.Notify(ctx, "info", "Reloading all MCP servers...")
	}

	p.mu.Lock()
	for name, s := range p.sessions {
		s.session.Close()
		if s.cmd != nil && s.cmd.Process != nil {
			s.cmd.Process.Kill()
		}
		delete(p.sessions, name)
	}
	p.mu.Unlock()

	if err := p.loadServers(ctx); err != nil {
		return "", fmt.Errorf("reload failed: %w", err)
	}

	p.mu.RLock()
	count := len(p.sessions)
	p.mu.RUnlock()

	if p.host != nil {
		_ = p.host.Notify(ctx, "info", fmt.Sprintf("Reloaded %d MCP server(s)", count))
	}
	return fmt.Sprintf("✓ reloaded %d MCP server(s)", count), nil
}

// connectServer connects to a single configured server.
func (p *MCPPlugin) connectServer(ctx context.Context, cfg serverConfig) error {
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "tau-plugin-mcp",
		Version: "0.1.0",
	}, nil)

	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", cfg.Name, err)
	}

	p.mu.Lock()
	p.sessions[cfg.Name] = &mcpSession{
		name:    cfg.Name,
		session: session,
		client:  client,
		cmd:     cmd,
	}
	p.mu.Unlock()

	p.logger.Info("tau-plugin-mcp: connected to server", "name", cfg.Name)
	return nil
}

// loadServers connects to all configured MCP servers.
func (p *MCPPlugin) loadServers(ctx context.Context) error {
	servers, err := p.readConfig()
	if err != nil {
		return fmt.Errorf("tau-plugin-mcp: read config: %w", err)
	}

	for _, cfg := range servers {
		if cfg.Command == "" {
			continue
		}
		if err := p.connectServer(ctx, cfg); err != nil {
			p.logger.Warn("tau-plugin-mcp: failed to connect to server",
				"name", cfg.Name, "command", cfg.Command, "err", err)
			continue
		}
	}

	return nil
}

// --- MCP server config ---

type serverConfig struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// These are loaded from the plugin's config. In a real implementation,
// this would come from the HostService.GetConfig() RPC. For now,
// read from environment variable or config file.
var serverConfigs = []serverConfig{}

func (p *MCPPlugin) readConfig() ([]serverConfig, error) {
	// Preferred source: the host's config subsystem via HostService.GetConfig.
	if p.host != nil {
		if v, found, err := p.host.GetConfig(context.Background(), "servers"); err == nil && found && v != "" {
			var servers []serverConfig
			if err := json.Unmarshal([]byte(v), &servers); err != nil {
				return nil, fmt.Errorf("parse plugins.mcp-plugin.servers: %w", err)
			}
			return servers, nil
		}
	}

	// Fallback: TAU_MCP_SERVERS env var (JSON) for setups without host config.
	if env := os.Getenv("TAU_MCP_SERVERS"); env != "" {
		var servers []serverConfig
		if err := json.Unmarshal([]byte(env), &servers); err != nil {
			return nil, fmt.Errorf("parse TAU_MCP_SERVERS: %w", err)
		}
		return servers, nil
	}
	return serverConfigs, nil
}

// --- MCP session helpers ---

func (s *mcpSession) listTools(ctx context.Context) ([]*pluginapi.ToolDefinition, error) {
	resp, err := s.session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return nil, err
	}

	tools := make([]*pluginapi.ToolDefinition, len(resp.Tools))
	for i, t := range resp.Tools {
		schema := ""
		if t.InputSchema != nil {
			schemaBytes, _ := json.Marshal(t.InputSchema)
			schema = string(schemaBytes)
		}
		tools[i] = &pluginapi.ToolDefinition{
			Name:        s.name + "." + t.Name,
			Description: t.Description,
			InputSchema: schema,
		}
	}
	return tools, nil
}

func (s *mcpSession) callTool(ctx context.Context, toolName, arguments string) (string, bool, error) {
	var args map[string]any
	if arguments != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return "", true, fmt.Errorf("tau-plugin-mcp: invalid arguments: %w", err)
		}
	}

	resp, err := s.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: args,
	})
	if err != nil {
		return "", true, fmt.Errorf("tau-plugin-mcp: call tool %q: %w", toolName, err)
	}

	if resp.IsError {
		var content strings.Builder
		for _, c := range resp.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				content.WriteString(tc.Text)
			}
		}
		return content.String(), true, nil
	}

	// Serialize content to JSON.
	var parts []map[string]any
	for _, c := range resp.Content {
		switch ct := c.(type) {
		case *mcp.TextContent:
			parts = append(parts, map[string]any{
				"type": "text",
				"text": ct.Text,
			})
		case *mcp.ImageContent:
			parts = append(parts, map[string]any{
				"type":     "image",
				"data":     ct.Data,
				"mimeType": ct.MIMEType,
			})
		}
	}

	result, _ := json.Marshal(parts)
	return string(result), false, nil
}

// compile-time check
var _ pluginapi.Extension = &MCPPlugin{}
