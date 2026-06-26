// Tau Hello Plugin — minimal example of the go-plugin extension API.
//
// Build:
//
//	cd plugins/hello
//	go build -o tau-plugin-hello .
//
// Install:
//
//	mkdir -p ~/.config/tau/plugins
//	cp tau-plugin-hello ~/.config/tau/plugins/
//
// Run tau and type "/hello world" or let the agent call the hello_greet tool.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"

	pluginapi "github.com/samcharles93/tau/pkg/plugin/api"
)

// HelloPlugin implements pluginapi.Extension. It exposes a slash command and
// a tool so both interactive and agentic usage are demonstrated.
type HelloPlugin struct {
	logger *slog.Logger
}

func main() {
	hclogger := hclog.New(&hclog.LoggerOptions{
		Level:      hclog.Info,
		Output:     os.Stderr,
		JSONFormat: false,
		Name:       "tau-plugin-hello",
	})

	p := &HelloPlugin{
		logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
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
		Logger:     hclogger,
	})
}

// Metadata returns the plugin name and any slash commands it provides.
func (p *HelloPlugin) Metadata() (string, []*pluginapi.Command) {
	return "hello", []*pluginapi.Command{
		{
			Name:          "/hello",
			Description:   "Say hello from a plugin: /hello [name]",
			ExtensionName: "hello",
		},
	}
}

// RunCommand executes a slash command registered by the plugin.
func (p *HelloPlugin) RunCommand(ctx context.Context, name, args string) (string, error) {
	if name != "/hello" {
		return "", fmt.Errorf("hello plugin: unknown command %q", name)
	}

	who := strings.TrimSpace(args)
	if who == "" {
		who = "tau"
	}
	return fmt.Sprintf("Hello, %s! 👋 (from hello plugin)", who), nil
}

// Reload is called when tau reloads extensions. The hello plugin has no state
// to refresh, so it simply returns successfully.
func (p *HelloPlugin) Reload(ctx context.Context) ([]*pluginapi.Diagnostic, []*pluginapi.Command, error) {
	return nil, nil, nil
}

// Tools returns the agent-callable tools provided by this plugin.
func (p *HelloPlugin) Tools(ctx context.Context) ([]*pluginapi.ToolDefinition, error) {
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Name to greet",
			},
			"enthusiasm": map[string]any{
				"type":        "integer",
				"description": "Number of exclamation marks",
				"default":     1,
			},
		},
		"required": []string{"name"},
	})

	return []*pluginapi.ToolDefinition{
		{
			Name:        "hello_greet",
			Description: "Greet someone enthusiastically from the hello plugin",
			InputSchema: string(schema),
		},
	}, nil
}

// ExecuteTool runs a tool previously advertised by Tools().
func (p *HelloPlugin) ExecuteTool(ctx context.Context, toolName, arguments string) (string, bool, error) {
	if toolName != "hello_greet" {
		return "", true, fmt.Errorf("hello plugin: unknown tool %q", toolName)
	}

	var args struct {
		Name       string `json:"name"`
		Enthusiasm int    `json:"enthusiasm"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", true, fmt.Errorf("hello plugin: parse arguments: %w", err)
	}

	if args.Enthusiasm < 1 {
		args.Enthusiasm = 1
	}
	if args.Enthusiasm > 10 {
		args.Enthusiasm = 10
	}

	bangs := strings.Repeat("!", args.Enthusiasm)
	return fmt.Sprintf("Hello, %s%s (from hello plugin)", args.Name, bangs), false, nil
}

// DispatchEvent receives lifecycle events from the coordinator. The hello
// plugin logs them and returns a nil response, taking no action.
func (p *HelloPlugin) DispatchEvent(ctx context.Context, event, sessionID string, payload *pluginapi.EventPayload) *pluginapi.EventResponse {
	p.logger.Info("lifecycle event", "event", event, "session_id", sessionID)
	return nil
}
