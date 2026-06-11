package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/agent"
	"github.com/samcharles93/tau/internal/agent/tools"
	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/plugin"
	"github.com/samcharles93/tau/internal/provider"
	"github.com/samcharles93/tau/internal/skills"
	"github.com/samcharles93/tau/internal/store"
	"github.com/samcharles93/tau/internal/tui"
	"github.com/samcharles93/tau/pkg/plugin/api"
)

// ChatOptions holds the parameters for launching an interactive chat session.
type ChatOptions struct {
	Config          tauconfig.Config
	Provider        tauconfig.ProviderConfig
	Insecure        bool
	Model           string
	MaxTokens       int
	Temperature     float64
	Version         string
	ResumeSessionID string
}

// printExitSummary prints session metadata after the TUI exits.
func printExitSummary(ctx context.Context, s store.SessionStore, sessionID, extra string) {
	summaries, _, err := s.List(ctx, 1, "")
	if err != nil || len(summaries) == 0 {
		return
	}
	// The most recent session is the one we just closed.
	latest := summaries[0]
	if latest.ID != sessionID {
		return
	}

	fmt.Fprintf(os.Stderr, "\nSession %s saved — %d messages, %s tokens",
		latest.ID, latest.MessageCount, formatTokensHuman(latest.TotalTokens))
	if latest.Cost > 0 {
		fmt.Fprintf(os.Stderr, ", $%.4f", latest.Cost)
	}
	fmt.Fprintf(os.Stderr, "\nResume: tau --resume %s\n", latest.ID)
	if extra != "" {
		fmt.Fprintln(os.Stderr, extra)
	}
}

func formatTokensHuman(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%dK", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

// buildModelRefresher returns a ModelRefresher closure that re-discovers
// models from the configured provider. The closure captures the provider and token so the TUI
// does not need to import infrastructure packages.
func buildModelRefresher(selectedProvider tauconfig.ProviderConfig, bearerToken string, insecure bool) tui.ModelRefresher {
	return func(ctx context.Context) ([]tauchat.ChatModelRef, error) {
		models, err := provider.DiscoverModels(ctx, selectedProvider, bearerToken, insecure)
		if err != nil {
			return nil, err
		}
		return buildModelRefs(models), nil
	}
}

func buildSessionConfig(opts ChatOptions, model tauchat.ChatModelRef, systemPrompt string) tauchat.ChatSessionConfig {
	return tauchat.ChatSessionConfig{
		Provider:     opts.Provider,
		Model:        model,
		SystemPrompt: systemPrompt,
		Parameters: tauchat.ChatParameters{
			MaxTokens:   opts.MaxTokens,
			Temperature: opts.Temperature,
		},
	}
}

// buildAgentSystemPrompt builds the full system prompt for the agent,
// combining project context (AGENTS.md), the skill catalog and working
// directory info
func buildAgentSystemPrompt(userPrompt, cwd string) string {
	contextFiles := agent.DiscoverContextFiles(cwd)

	sources := skills.DefaultSources(cwd)
	allSkills, _ := skills.Discover(sources)
	activeSkills := skills.FilterDisabled(allSkills, nil)

	return agent.BuildSystemPrompt(agent.PromptConfig{
		ContextFiles: contextFiles,
		Skills:       activeSkills,
		CWD:          cwd,
		AppendPrompt: userPrompt,
	})
}

func pickModel(models []provider.Model, requestedModel, defaultModel, baseURL string) (tauchat.ChatModelRef, error) {
	selectedModel := strings.TrimSpace(requestedModel)
	if selectedModel == "" {
		selectedModel = strings.TrimSpace(defaultModel)
	}
	if selectedModel == "" {
		return tauchat.ChatModelRef{}, errors.New("chat model is required; pass --model or set default_model")
	}

	for _, m := range models {
		if m.ID != selectedModel {
			continue
		}
		if !m.Ready {
			return tauchat.ChatModelRef{}, fmt.Errorf("model %q is not ready", selectedModel)
		}
		return tauchat.ChatModelRef{
			ID:     m.ID,
			URL:    m.URL,
			Ready:  m.Ready,
			Config: m.Config,
		}, nil
	}

	return tauchat.ChatModelRef{ID: selectedModel, URL: strings.TrimRight(baseURL, "/"), Ready: true}, nil
}

func buildModelRefs(models []provider.Model) []tauchat.ChatModelRef {
	refs := make([]tauchat.ChatModelRef, 0, len(models))
	for _, m := range models {
		refs = append(refs, tauchat.ChatModelRef{
			ID:     m.ID,
			URL:    m.URL,
			Ready:  m.Ready,
			Config: m.Config,
		})
	}
	return refs
}

// newCoordinator creates and returns an agent coordinator with the standard
// tool registry, config, and session persistence.
func newCoordinator(ctx context.Context, opts ChatOptions, bearerToken string, sessionStore store.SessionStore, startupEvents []tauchat.ChatEvent, bus *eventbus.Bus) (*agent.Coordinator, error) {
	return buildCoordinator(ctx, coordinatorConfig{
		Bus:           bus,
		ChatOptions:   opts,
		BearerToken:   bearerToken,
		SessionStore:  sessionStore,
		InteractiveUI: true,
		StartupEvents: startupEvents,
	})
}

// coordinatorConfig holds all parameters for building a coordinator instance,
// shared between interactive and headless modes.
type coordinatorConfig struct {
	Bus           *eventbus.Bus
	ChatOptions   ChatOptions
	BearerToken   string
	SessionStore  store.SessionStore
	InteractiveUI bool
	StartupEvents []tauchat.ChatEvent
}

// buildCoordinator creates a coordinator with the full plugin/tool setup.
func buildCoordinator(ctx context.Context, cfg coordinatorConfig) (*agent.Coordinator, error) {
	cwd, _ := os.Getwd()
	registry := tools.NewRegistry()
	if err := tools.RegisterBuiltins(registry, cwd); err != nil {
		return nil, fmt.Errorf("registering built-in tools: %w", err)
	}

	// Plugin manager — discovers and manages extension binaries.
	pluginMgr, err := plugin.NewManager(plugin.Config{
		ToolRegistry: registry,
		Logger:       slog.Default(),
	})
	if err != nil {
		return nil, fmt.Errorf("plugin manager: %w", err)
	}

	registry.SetPluginToolExecutor(func(ctx context.Context, pluginName, toolName string, args json.RawMessage) (tools.Result, error) {
		return pluginMgr.ExecutePluginTool(ctx, pluginName, toolName, args)
	})

	if err := pluginMgr.Load(ctx); err != nil {
		slog.Warn("plugin manager load failed", "err", err)
	}

	// Subscribe the plugin manager to fire-and-forget lifecycle events
	// published on the bus. Request-response events (context,
	// before_llm_call, before_tool_exec, after_tool_exec) continue
	// through the OnPluginEvent callback below.
	pluginBusClient := cfg.Bus.Client("plugin-manager")
	eventbus.SubscribeFunc(pluginBusClient, func(evt tauchat.PluginLifecycleEvent) {
		payload, _ := evt.Payload.(*api.EventPayload)
		pluginMgr.DispatchEvent(ctx, evt.Event, evt.SessionID, payload)
	})

	// Schedule ticks: if TAU_SCHEDULE_INTERVAL is set, publish a
	// ScheduleTickEvent on the bus at that interval. The plugin manager
	// (and any other subscriber) receives these for background work.
	scheduleInterval := tauconfig.ScheduleIntervalFromEnv()
	if scheduleInterval > 0 {
		slog.Info("schedule events enabled", "interval", scheduleInterval)
		schedulePub := eventbus.Publish[tauchat.ScheduleTickEvent](pluginBusClient)
		eventbus.SubscribeFunc(pluginBusClient, func(evt tauchat.ScheduleTickEvent) {
			pluginMgr.DispatchEvent(ctx, "schedule", "", nil)
		})
		go func() {
			ticker := time.NewTicker(scheduleInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case now := <-ticker.C:
					schedulePub.Publish(tauchat.ScheduleTickEvent{OccurredAt: now.UTC()})
				}
			}
		}()
	}

	coordinator, err := agent.NewCoordinator(ctx, agent.CoordinatorConfig{
		Bus:               cfg.Bus,
		TokenSource:       staticTokenSource(cfg.BearerToken),
		Streamer:          provider.OpenAIStreamer{Insecure: cfg.ChatOptions.Insecure},
		Registry:          registry,
		InteractiveUI:     cfg.InteractiveUI,
		SessionStore:      cfg.SessionStore,
		AutoExportJSONL:   true,
		StartupEvents:     cfg.StartupEvents,
		ExtensionReloader: pluginMgr,
		OnPluginEvent: func(event string, sessionID string, payload *api.EventPayload) *api.EventResponse {
			return pluginMgr.DispatchEvent(ctx, event, sessionID, payload)
		},
		OnClose: func() {
			pluginMgr.Unload()
		},
	})
	if err != nil {
		return nil, err
	}
	return coordinator, nil
}

func staticTokenSource(token string) provider.TokenSource {
	trimmed := strings.TrimSpace(token)
	return func(ctx context.Context, _ tauconfig.ProviderConfig) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		if trimmed == "" {
			return "", nil
		}
		return trimmed, nil
	}
}

func isDevel(version string, cfg tauconfig.Config) bool {
	if cfg.Debug {
		return true
	}
	v := strings.ToLower(version)
	return strings.Contains(v, "dev") || strings.Contains(v, "none")
}
