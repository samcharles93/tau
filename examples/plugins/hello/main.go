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
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"

	pluginapi "github.com/samcharles93/tau/pkg/plugin/api"
)

//go:embed docs.md
var docsFS embed.FS

// watchViewID is the panel id used by /hello watch and /hello close. Re-
// rendering a view with the same id replaces its content in place, which is
// what lets the watch goroutine push live updates without creating new panels.
const watchViewID = "watch-demo"

// HelloPlugin implements pluginapi.Extension. It exposes a slash command, a
// tool, and a demo panel so command, tool, and view usage are all
// demonstrated. It also implements pluginapi.HostAware, pluginapi.Capable,
// and pluginapi.Documented to demonstrate calling back into the host,
// declaring capabilities, and surfacing its own docs to tau's docs tool.
type HelloPlugin struct {
	logger *slog.Logger
	host   pluginapi.Host

	watchMu     sync.Mutex
	watchCancel context.CancelFunc // cancels the live-watch goroutine
}

// SetHost implements pluginapi.HostAware.
func (p *HelloPlugin) SetHost(h pluginapi.Host) { p.host = h }

// Capabilities implements pluginapi.Capable.
func (p *HelloPlugin) Capabilities() []string {
	return []string{
		pluginapi.CapabilityCommands,
		pluginapi.CapabilityTools,
		pluginapi.CapabilityEvents,
		pluginapi.CapabilityViews,
	}
}

// Docs implements pluginapi.Documented, surfacing docs.md through tau's
// docs tool under the virtual path "plugins/hello.md".
func (p *HelloPlugin) Docs() string {
	content, err := docsFS.ReadFile("docs.md")
	if err != nil {
		return ""
	}
	return string(content)
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
			Name:          "hello",
			Description:   "Say hello from a plugin: /hello [name], /hello panel, /hello watch, /hello close",
			ExtensionName: "hello",
			Subcommands: []*pluginapi.Command{
				{Name: "panel", Description: "Render a one-shot demo panel", ExtensionName: "hello"},
				{Name: "watch", Description: "Open (or update) a live demo panel", ExtensionName: "hello"},
				{Name: "close", Description: "Close the panel opened by /hello watch", ExtensionName: "hello"},
				{Name: "ask", Description: "Demo: ask the user a free-text question via host.Input", ExtensionName: "hello"},
				{Name: "confirm", Description: "Demo: ask the user to confirm via host.Confirm", ExtensionName: "hello"},
			},
		},
	}
}

// RunCommand executes a slash command registered by the plugin. name is the
// space-joined command path (e.g. "hello" or "hello panel"), matching
// whichever entry in Metadata's Subcommands the user invoked.
func (p *HelloPlugin) RunCommand(ctx context.Context, name, args string) (string, *pluginapi.View, error) {
	switch name {
	case "hello":
		who := strings.TrimSpace(args)
		if who == "" {
			who = "tau"
		}
		return fmt.Sprintf("Hello, %s! 👋 (from hello plugin)", who), nil, nil

	case "hello panel":
		// Sync path: the view is attached directly to the command result.
		return "", demoPanel("hello-panel", "one-shot"), nil

	case "hello watch":
		// Async path: push a persistent panel via HostService.RenderView,
		// then start a goroutine that re-pushes every second so the
		// rendered_at timestamp and status ticker visibly update.
		// Re-sending a View with the same Id replaces in place.
		if p.host == nil {
			return "", nil, fmt.Errorf("hello plugin: host not available")
		}
		p.startWatch()
		return "opened watch panel — it refreshes every second; /hello close to close it", nil, nil

	case "hello close":
		if p.host == nil {
			return "", nil, fmt.Errorf("hello plugin: host not available")
		}
		p.stopWatch()
		if err := p.host.CloseView(ctx, watchViewID); err != nil {
			return "", nil, fmt.Errorf("hello plugin: close view: %w", err)
		}
		return "closed watch panel", nil, nil

	case "hello ask":
		if p.host == nil {
			return "", nil, fmt.Errorf("hello plugin: host not available")
		}
		answer, err := p.host.Input(ctx, "Hello plugin asks", "e.g. your name")
		if err != nil {
			return "", nil, fmt.Errorf("hello plugin: input canceled or failed: %w", err)
		}
		return fmt.Sprintf("you answered: %q", answer), nil, nil

	case "hello confirm":
		if p.host == nil {
			return "", nil, fmt.Errorf("hello plugin: host not available")
		}
		ok, err := p.host.Confirm(ctx, "Hello plugin confirms", "Do you want to proceed?")
		if err != nil {
			return "", nil, fmt.Errorf("hello plugin: confirm canceled or failed: %w", err)
		}
		return fmt.Sprintf("you confirmed: %v", ok), nil, nil

	default:
		return "", nil, fmt.Errorf("hello plugin: unknown command %q", name)
	}
}

// startWatch starts a goroutine that pushes a fresh panel via RenderView every
// second, demonstrating genuinely live self-updating panels. It cancels any
// previously running watch goroutine so only one is active at a time.
func (p *HelloPlugin) startWatch() {
	p.stopWatch()
	// Use an independent context, not the gRPC call context — that one is
	// cancelled as soon as RunCommand returns, which would kill the watch
	// goroutine before its first RenderView lands.
	ctx, cancel := context.WithCancel(context.Background())
	p.watchMu.Lock()
	p.watchCancel = cancel
	p.watchMu.Unlock()
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			// First push is immediate so the panel appears without a 1s gap.
			if err := p.host.RenderView(ctx, livePanel()); err != nil {
				p.logger.Warn("hello plugin: watch panel render failed", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (p *HelloPlugin) stopWatch() {
	p.watchMu.Lock()
	defer p.watchMu.Unlock()
	if p.watchCancel != nil {
		p.watchCancel()
		p.watchCancel = nil
	}
}

// livePanel builds the auto-refreshing panel with a RUNNING status so the
// spinner glyph animates, plus an always-fresh rendered_at timestamp.
func livePanel() *pluginapi.View {
	return &pluginapi.View{
		Id:    watchViewID,
		Title: "Hello Plugin — Live Panel",
		Widgets: []*pluginapi.Widget{
			{Kind: &pluginapi.Widget_KeyValue{KeyValue: &pluginapi.KeyValueWidget{
				Entries: []*pluginapi.KeyValueWidget_Entry{
					{Key: "rendered_at", Value: time.Now().Format(time.TimeOnly)},
					{Key: "refresh_rate", Value: "1s"},
				},
			}}},
			{Kind: &pluginapi.Widget_Status{Status: &pluginapi.StatusWidget{
				State:  pluginapi.StatusWidget_RUNNING,
				Label:  "hello plugin",
				Detail: "live panel auto-refreshing",
			}}},
		},
	}
}

func demoPanel(id, kind string) *pluginapi.View {
	return &pluginapi.View{
		Id:    id,
		Title: "Hello Plugin Demo Panel",
		Widgets: []*pluginapi.Widget{
			{Kind: &pluginapi.Widget_KeyValue{KeyValue: &pluginapi.KeyValueWidget{
				Entries: []*pluginapi.KeyValueWidget_Entry{
					{Key: "kind", Value: kind},
					{Key: "rendered_at", Value: time.Now().Format(time.RFC3339)},
				},
			}}},
			{Kind: &pluginapi.Widget_Status{Status: &pluginapi.StatusWidget{
				State:  pluginapi.StatusWidget_SUCCESS,
				Label:  "hello plugin",
				Detail: "panel rendering works",
			}}},
		},
	}
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
