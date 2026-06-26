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

	"github.com/pkg/browser"
	"github.com/samcharles93/ai-sdk/pkg/runtime"

	"github.com/samcharles93/tau/internal/agent"
	"github.com/samcharles93/tau/internal/agent/tools"
	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/plugin"
	commandreg "github.com/samcharles93/tau/internal/registry"
	"github.com/samcharles93/tau/internal/sessions"
	"github.com/samcharles93/tau/internal/skills"
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
	ReasoningEffort string
	Version         string
	ResumeSessionID string
	Web             bool // --web: auto-open browser
	WebPort         int  // --port: 0 = ephemeral
	NoWeb           bool // --no-web: do not start web UI
}

// printExitSummary prints session metadata after the TUI exits.
func printExitSummary(ctx context.Context, m *sessions.Manager, sessionID, extra string) {
	summaries, _, err := m.List(ctx, 1, "")
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
// models from the configured provider. The closure captures the provider and
// insecure flag so the TUI does not need to import infrastructure packages.
func buildModelRefresher(rt *runtime.Runtime, providerID, baseURL string) tui.ModelRefresher {
	return func(ctx context.Context) ([]tauchat.ChatModelRef, error) {
		if err := rt.Catalog().Fetch(ctx); err != nil {
			return nil, err
		}
		models, err := rt.Models(providerID)
		if err != nil {
			return nil, err
		}
		return modelInfoRefs(models, baseURL), nil
	}
}

func buildSessionConfig(opts ChatOptions, model tauchat.ChatModelRef, systemPrompt string) tauchat.ChatSessionConfig {
	maxTokens := opts.MaxTokens
	if maxTokens == 0 && model.Config.DefaultMaxTokens > 0 {
		maxTokens = model.Config.DefaultMaxTokens
	}
	reasoningEffort := opts.ReasoningEffort
	if reasoningEffort == "" && model.Config.ReasoningEffort != "" {
		reasoningEffort = model.Config.ReasoningEffort
	}
	return tauchat.ChatSessionConfig{
		Provider:     opts.Provider,
		Model:        model,
		SystemPrompt: systemPrompt,
		Parameters: tauchat.ChatParameters{
			MaxTokens:       maxTokens,
			Temperature:     opts.Temperature,
			ReasoningEffort: reasoningEffort,
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

func pickModel(models []runtime.ModelInfo, requestedModel, defaultModel, baseURL string) (tauchat.ChatModelRef, error) {
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
		return modelInfoToRef(m, baseURL), nil
	}

	return tauchat.ChatModelRef{ID: selectedModel, URL: strings.TrimRight(baseURL, "/")}, nil
}

func modelInfoRefs(models []runtime.ModelInfo, baseURL string) []tauchat.ChatModelRef {
	refs := make([]tauchat.ChatModelRef, 0, len(models))
	for _, m := range models {
		refs = append(refs, modelInfoToRef(m, baseURL))
	}
	return refs
}

func modelInfoToRef(m runtime.ModelInfo, baseURL string) tauchat.ChatModelRef {
	// Catalogue models often carry no per-model URL; fall back to the provider
	// base URL so the model reference passes validation when switching models.
	url := strings.TrimRight(m.URL, "/")
	if url == "" {
		url = strings.TrimRight(baseURL, "/")
	}
	return tauchat.ChatModelRef{
		ID:     m.ID,
		URL:    url,
		Config: modelInfoToModelConfig(m),
	}
}

func modelInfoToModelConfig(m runtime.ModelInfo) tauconfig.ModelConfig {
	cfg := tauconfig.ModelConfig{
		ID:               m.ID,
		Name:             m.Name,
		ContextWindow:    m.ContextWindow,
		DefaultMaxTokens: m.MaxOutputTokens,
		MaxTokens:        m.MaxOutputTokens,
		Reasoning:        m.Reasoning,
		Cost: tauconfig.CostConfig{
			Input:      m.Cost.Input,
			Output:     m.Cost.Output,
			CacheRead:  m.Cost.CacheRead,
			CacheWrite: m.Cost.CacheWrite,
		},
	}
	// Restore compat config if it was stored during runtime construction.
	if raw, ok := m.Extra["tau_compat"]; ok {
		if compat, ok := raw.(tauconfig.CompatConfig); ok {
			cfg.Compat = compat
		}
	}
	// Restore reasoning effort from Extra.
	if eff, ok := m.Extra["reasoning_effort"].(string); ok {
		cfg.ReasoningEffort = eff
	}
	return cfg
}

// buildStreamer constructs an ai-sdk-backed streamer for the selected
// provider/model. The runtime must be configured with the provider before
// calling.
func buildStreamer(
	ctx context.Context,
	rt *runtime.Runtime,
	providerName string,
	model tauchat.ChatModelRef,
) (agent.Streamer, error) {
	ref := providerName + "/" + model.ID
	provider, modelID, err := rt.ChatProvider(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("resolving provider for %q: %w", ref, err)
	}
	return NewStreamer(provider, modelID), nil
}

// resolveProviderClass maps a tau provider config to an ai-sdk runtime class.
// The config's `type` is honoured only when it names a registered class (e.g.
// `type: deepseek`). tau's deployment kinds — `hosted`, `local` — are not
// runtime classes, so any provider speaking the OpenAI chat-completions dialect
// (the `api: openai-completions` default) resolves to the generic
// openai-compatible class.
func resolveProviderClass(provider tauconfig.ProviderConfig) string {
	if t := strings.TrimSpace(provider.Type); t != "" {
		if _, ok := runtime.GetClass(t); ok {
			return t
		}
	}
	return "openai-compatible"
}

// newRuntimeForProvider creates a runtime configured for a tau provider.
// It maps tau's provider configuration to the ai-sdk runtime config and
// registers the built-in provider classes. The provider class is resolved
// by [resolveProviderClass].
func newRuntimeForProvider(provider tauconfig.ProviderConfig, insecure bool) *runtime.Runtime {
	runtime.RegisterBuiltinClasses()

	authType := runtime.AuthTypeAPIKey
	switch provider.Auth.Type {
	case tauconfig.AuthTypeNone:
		authType = runtime.AuthTypeNone
	case tauconfig.AuthTypeOAuthPKCE:
		authType = runtime.AuthTypeOAuthPKCE
	}

	models := make([]runtime.ModelConfig, 0, len(provider.Models))
	for _, m := range provider.Models {
		extra := make(map[string]any)
		// Store compat config that runtime.ModelInfo doesn't natively carry.
		extra["tau_compat"] = m.Compat
		if m.ReasoningEffort != "" {
			extra["reasoning_effort"] = m.ReasoningEffort
		}
		models = append(models, runtime.ModelConfig{
			ID:              m.ID,
			Name:            m.Name,
			URL:             m.URL,
			ContextWindow:   m.ContextWindow,
			MaxOutputTokens: m.DefaultMaxTokens,
			Reasoning:       m.Reasoning,
			Extra:           extra,
		})
	}

	return runtime.NewRuntime(runtime.Config{
		Providers: map[string]runtime.ProviderConfig{
			provider.Name: {
				ID:       provider.Name,
				Class:    resolveProviderClass(provider),
				BaseURL:  strings.TrimRight(provider.BaseURL, "/"),
				Insecure: insecure,
				Auth: runtime.AuthConfig{
					Type:            authType,
					APIKeyEnv:       provider.Auth.APIKeyEnv,
					APIKey:          provider.Auth.APIKey,
					AuthorizeURL:    provider.Auth.AuthorizeURL,
					TokenURL:        provider.Auth.TokenURL,
					ClientID:        provider.Auth.ClientID,
					IDP:             provider.Auth.IDP,
					TokenAuthMethod: provider.Auth.TokenAuthMethod,
				},
				Models: models,
			},
		},
	})
}

// newCoordinatorResult bundles a coordinator with its command registry so
// the caller can seed the TUI with the initial command snapshot before the
// bus delivers the first CommandsChangedEvent.
type newCoordinatorResult struct {
	Coordinator     *agent.Coordinator
	CommandRegistry *commandreg.Registry
}

// newCoordinator creates and returns an agent coordinator with the standard
// tool registry, config, and session persistence.
func newCoordinator(ctx context.Context, opts ChatOptions, bearerToken string, sessionManager *sessions.Manager, startupEvents []tauchat.ChatEvent, bus *eventbus.Bus, streamer agent.Streamer) (*newCoordinatorResult, error) {
	cwd, _ := os.Getwd()
	cmdRegClient := bus.Client("command-registry")
	cmdReg := commandreg.New(cwd, cmdRegClient)

	coordinator, err := buildCoordinator(ctx, coordinatorConfig{
		Bus:             bus,
		ChatOptions:     opts,
		BearerToken:     bearerToken,
		SessionManager:  sessionManager,
		InteractiveUI:   true,
		StartupEvents:   startupEvents,
		CommandRegistry: cmdReg,
		AutoExportJSONL: true,
		Streamer:        streamer,
	})
	if err != nil {
		cmdReg.Close()
		return nil, err
	}
	return &newCoordinatorResult{
		Coordinator:     coordinator,
		CommandRegistry: cmdReg,
	}, nil
}

// coordinatorConfig holds all parameters for building a coordinator instance,
// shared between interactive and headless modes.
type coordinatorConfig struct {
	Bus             *eventbus.Bus
	ChatOptions     ChatOptions
	BearerToken     string
	SessionManager  *sessions.Manager
	InteractiveUI   bool
	StartupEvents   []tauchat.ChatEvent
	CommandRegistry *commandreg.Registry
	AutoExportJSONL bool
	Streamer        agent.Streamer
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

	// Command registry — the caller creates this and passes it in so the
	// initial command snapshot can be seeded into the TUI before the bus
	// delivers the first CommandsChangedEvent.
	if cfg.CommandRegistry != nil {
		cfg.CommandRegistry.Discover()
		// Discover skills once and share with the registry to avoid
		// redundant filesystem walks (the prompt builder also uses
		// skill discovery).
		allSkills, _ := skills.Discover(skills.DefaultSources(cwd))
		cfg.CommandRegistry.MergeSkills(allSkills)
	}

	coordinator, err := agent.NewCoordinator(ctx, agent.CoordinatorConfig{
		Bus:               cfg.Bus,
		TokenSource:       staticTokenSource(cfg.BearerToken),
		Streamer:          cfg.Streamer,
		Registry:          registry,
		InteractiveUI:     cfg.InteractiveUI,
		SessionManager:    cfg.SessionManager,
		AutoExportJSONL:   cfg.AutoExportJSONL,
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

func staticTokenSource(token string) agent.TokenSource {
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

// openBrowser opens url in the user's default browser.
func openBrowser(ctx context.Context, url string) error {
	return browserOpenURL(ctx, url)
}

// browserOpenURL is a test seam.
var browserOpenURL = defaultBrowserOpenURL

func defaultBrowserOpenURL(ctx context.Context, url string) error {
	_ = ctx
	return browser.OpenURL(url)
}
