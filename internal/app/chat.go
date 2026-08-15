package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/pkg/browser"
	aisdkchat "github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/runtime"

	"github.com/samcharles93/tau/internal/agent"
	"github.com/samcharles93/tau/internal/agent/tools"
	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/indexing"
	taulogger "github.com/samcharles93/tau/internal/logger"
	"github.com/samcharles93/tau/internal/plugin"
	"github.com/samcharles93/tau/internal/providers"
	"github.com/samcharles93/tau/internal/providers/snapshot"
	commandreg "github.com/samcharles93/tau/internal/registry"
	"github.com/samcharles93/tau/internal/sessions"
	"github.com/samcharles93/tau/internal/skills"
	"github.com/samcharles93/tau/internal/store"
	"github.com/samcharles93/tau/internal/tui"
	"github.com/samcharles93/tau/pkg/plugin/api"
)

// ChatOptions holds the parameters for launching an interactive chat session.
type ChatOptions struct {
	Config               tauconfig.Config
	Provider             tauconfig.ProviderConfig
	Insecure             bool
	Model                string
	MaxTokens            int
	Temperature          float64
	ReasoningEffort      string
	Version              string
	ResumeSessionID      string
	Web                  bool     // --web: auto-open browser
	WebPort              int      // --port: 0 = ephemeral
	NoWeb                bool     // --no-web: do not start web UI
	SkillDirs            []string // --skill-dir: additional skill directories
	NewTUI               bool     // default true; --legacy-tui inverts to use the legacy inline TUI
	Ephemeral            bool     // --ephemeral: do not persist this session to the session store
	AllowedTools         []string // --tools: initial tool allowlist (empty = unrestricted)
	AgentSpec            string   // --agent: spec name for the root agent identity (default "tau")
	TrustProjectRootSpec bool

	// OutputFormat selects the event output format for -p (stdin) mode.
	// "" or "plain" produces the default human-readable text; "jsonl" produces
	// framed JSONL on stdout via JSONLRenderer.
	OutputFormat string
	// Logger is the root logger for the session. When nil, slog.Default() is
	// used. Each subsystem derives a named child (component=xxx) from it.
	Logger *slog.Logger
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
	// Nothing was ever sent - there's no conversation to save or resume,
	// so claiming "saved" and offering a resume hint would be misleading.
	if latest.MessageCount == 0 {
		return
	}

	fmt.Fprintf(os.Stderr, "\nSession %s saved - %d messages, %s tokens",
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
// models across every supplied provider, tagging each model with the provider
// it came from. The aggregated list is what powers cross-provider /model
// selection. The closure captures the providers so the TUI does not need to
// import infrastructure packages.
func buildModelRefresher(pr *providerRuntime) tui.ModelRefresher {
	return func(ctx context.Context) ([]tauchat.ChatModelRef, error) {
		// Rebuild the runtime from current provider state so models from
		// providers enabled since launch (via /provider) appear, and the dynamic
		// streamer can route to them.
		if err := pr.reload(ctx); err != nil {
			return nil, err
		}
		rt, provs := pr.snapshot()
		return aggregateModelRefs(ctx, rt, pr.insecure, provs), nil
	}
}

// aggregateModelRefs collects model references from every provider, tagging
// each with its provider. Providers flagged for live discovery (e.g. a local
// Ollama server) are listed from their /models endpoint at runtime; the rest
// come from the embedded snapshot, filtered to tool-capable models. Providers
// that fail to enumerate are skipped rather than failing the whole list.
func aggregateModelRefs(ctx context.Context, rt *runtime.Runtime, insecure bool, provs []tauconfig.ProviderConfig) []tauchat.ChatModelRef {
	var out []tauchat.ChatModelRef
	for _, p := range provs {
		if p.Name == "openai-codex" {
			refs, err := codexModelRefs(ctx, p, insecure)
			if err != nil {
				slog.Debug("Codex model discovery failed", "provider", p.Name, "err", err)
				continue
			}
			out = append(out, refs...)
			continue
		}
		if entry, ok := providers.Lookup(p.Name); ok && entry.LiveModels {
			refs, err := liveModelRefs(ctx, p, insecure)
			if err != nil {
				slog.Debug("live model discovery failed", "provider", p.Name, "err", err)
				continue
			}
			out = append(out, refs...)
			continue
		}
		models, err := rt.Models(p.Name)
		if err != nil {
			slog.Debug("model enumeration failed", "provider", p.Name, "err", err)
			continue
		}
		out = append(out, modelInfoRefs(toolCapable(models), p.Name, p.BaseURL)...)
	}
	return out
}

// toolCapable keeps only models that advertise tool / function calling - the
// minimum capability tau's agent loop needs, so models that can't call tools
// never clutter the picker. If none of a provider's models advertise it (the
// catalogue carries no capability data for that provider), the full list is
// returned rather than rendering the provider empty.
func toolCapable(models []runtime.ModelInfo) []runtime.ModelInfo {
	filtered := make([]runtime.ModelInfo, 0, len(models))
	for _, m := range models {
		if m.ToolCall {
			filtered = append(filtered, m)
		}
	}
	if len(filtered) == 0 {
		return models
	}
	return filtered
}

func buildSessionConfig(opts ChatOptions, model tauchat.ChatModelRef, systemPrompt string) tauchat.ChatSessionConfig {
	maxTokens := opts.MaxTokens
	if maxTokens == 0 && model.Config.DefaultMaxTokens > 0 {
		maxTokens = model.Config.DefaultMaxTokens
	}
	maxTokens = tauchat.ClampMaxTokensForModel(maxTokens, model)
	reasoningEffort := opts.ReasoningEffort
	if reasoningEffort == "" && model.Config.ReasoningEffort != "" {
		reasoningEffort = model.Config.ReasoningEffort
	}
	// Default to "medium" for models that support reasoning effort.
	if reasoningEffort == "" && len(model.Config.ReasoningEfforts) > 0 {
		if slices.Contains(model.Config.ReasoningEfforts, "medium") {
			reasoningEffort = "medium"
		} else {
			reasoningEffort = model.Config.ReasoningEfforts[0]
		}
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
// directory info. toolSchemas are the built-in tool name/description pairs
// rendered into the prompt as capability metadata.
func buildAgentSystemPrompt(cwd string, skillsMgr *skills.Manager, toolSchemas []tools.Schema) string {
	contextFiles := agent.DiscoverContextFiles(cwd)

	var activeSkills []*skills.Skill
	if skillsMgr != nil {
		activeSkills = skillsMgr.Snapshot().ActiveSkills
	} else {
		sources := skills.DefaultSources(cwd)
		allSkills, _ := skills.Discover(sources)
		activeSkills = skills.FilterDisabled(allSkills, nil)
	}

	return agent.BuildSystemPrompt(agent.PromptConfig{
		ContextFiles: contextFiles,
		Skills:       activeSkills,
		CWD:          cwd,
		Tools:        toolSchemas,
	})
}

// pickModel resolves the requested or default model to a full reference. When
// neither a --model flag nor a default_model is set it returns a zero ref (and
// no error): the interactive session launches unselected and the user chooses a
// model with /model. Headless callers that require a model must check for an
// empty ID themselves.
func pickModel(models []runtime.ModelInfo, requestedModel, defaultModel, provider, baseURL string) (tauchat.ChatModelRef, error) {
	selectedModel := strings.TrimSpace(requestedModel)
	if selectedModel == "" {
		selectedModel = strings.TrimSpace(defaultModel)
	}
	if selectedModel == "" {
		return tauchat.ChatModelRef{}, nil
	}

	for _, m := range models {
		if m.ID != selectedModel {
			continue
		}
		return modelInfoToRef(m, provider, baseURL), nil
	}

	return tauchat.ChatModelRef{ID: selectedModel, URL: strings.TrimRight(baseURL, "/"), Provider: provider}, nil
}

func modelInfoRefs(models []runtime.ModelInfo, provider, baseURL string) []tauchat.ChatModelRef {
	refs := make([]tauchat.ChatModelRef, 0, len(models))
	for _, m := range models {
		refs = append(refs, modelInfoToRef(m, provider, baseURL))
	}
	return refs
}

func modelInfoToRef(m runtime.ModelInfo, provider, baseURL string) tauchat.ChatModelRef {
	// Catalogue models often carry no per-model URL; fall back to the provider
	// base URL so the model reference passes validation when switching models.
	url := strings.TrimRight(m.URL, "/")
	if url == "" {
		url = strings.TrimRight(baseURL, "/")
	}
	return tauchat.ChatModelRef{
		ID:       m.ID,
		URL:      url,
		Provider: provider,
		Config:   modelInfoToModelConfig(m),
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
	// Carry the model's selectable reasoning effort levels (models.dev
	// reasoning_options of type "effort") so the TUI can offer exactly those.
	for _, opt := range m.ReasoningOptions {
		if strings.EqualFold(opt.Type, "effort") && len(opt.Values) > 0 {
			cfg.ReasoningEfforts = append(cfg.ReasoningEfforts, opt.Values...)
		}
	}
	// For reasoning models that don't advertise explicit effort-type
	// options, compute synthetic effort levels from the model's output
	// budget so the TUI still offers low/medium/high.
	if m.Reasoning && len(cfg.ReasoningEfforts) == 0 {
		maxBudget := m.MaxOutputTokens
		if maxBudget <= 0 {
			maxBudget = 8192 // sensible floor
		}
		cfg.ReasoningEfforts = []string{"low", "medium", "high"}
		cfg.ReasoningBudgetMax = maxBudget
	}
	return cfg
}

// buildDynamicStreamer constructs a streamer that resolves its provider/model
// per turn from the session state, against a runtime configured with every
// usable provider. Switching model or provider mid-session (via /model or
// /provider) therefore takes effect on the next turn with no coordinator
// rebuild. When no provider/model is selected it returns a friendly error
// pointing the user at /provider and /model.
func buildDynamicStreamer(pr *providerRuntime) agent.Streamer {
	return NewDynamicStreamer(func(ctx context.Context, session tauchat.ChatSessionState) (aisdkchat.Provider, string, error) {
		providerName := strings.TrimSpace(session.Provider.Name)
		modelID := strings.TrimSpace(session.Model.ID)
		rt, configuredProviders := pr.snapshot()
		if providerName == "" || len(configuredProviders) == 0 {
			return nil, "", errors.New("no provider selected: enable a provider with /provider, then choose a model with /model")
		}
		if modelID == "" {
			return nil, "", errors.New("no model selected - enable a provider with /provider, then choose a model with /model")
		}
		ref := providerName + "/" + modelID
		provider, resolvedID, err := rt.ChatProvider(ctx, ref)
		if err != nil {
			return nil, "", fmt.Errorf("resolving provider for %q: %w", ref, err)
		}
		return provider, resolvedID, nil
	})
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
// The config's `type` is honoured when it names a registered class (e.g.
// `type: gemini`). Otherwise tau's curated catalog supplies an OpenAI-compatible
// base URL for every provider, so the generic openai-compatible class is the
// correct, coherent default. We deliberately do not derive the class from the
// models.dev npm mapping: that data is inconsistent (id mismatches, unmapped
// packages) and would pair a non-OpenAI client with our OpenAI-style base URLs.
// tau's deployment kinds - `hosted`, `local` - are not runtime classes and fall
// through to the default here.
func resolveProviderClass(provider tauconfig.ProviderConfig) string {
	if t := strings.TrimSpace(provider.Type); t != "" {
		if _, ok := runtime.GetClass(t); ok {
			return t
		}
	}
	// Compatibility for older DeepSeek configs that predate CatalogEntry.Class.
	// Do not infer this broadly from every provider name: Tau intentionally
	// routes providers such as Ollama through their OpenAI-compatible /v1
	// surfaces, not ai-sdk's native provider classes.
	if strings.TrimSpace(provider.Name) == "deepseek" {
		return "deepseek"
	}
	return "openai-compatible"
}

func runtimeProviderBaseURL(provider tauconfig.ProviderConfig, class string) string {
	baseURL := strings.TrimRight(provider.BaseURL, "/")
	if class == "deepseek" {
		baseURL = strings.TrimSuffix(baseURL, "/v1")
	}
	return baseURL
}

// newRuntimeForProvider creates a runtime configured for a single tau provider.
// It is a convenience wrapper over [newRuntimeForProviders] for headless paths
// that only ever talk to one provider.
func newRuntimeForProvider(provider tauconfig.ProviderConfig, insecure bool) *runtime.Runtime {
	return newRuntimeForProviders([]tauconfig.ProviderConfig{provider}, insecure)
}

// newRuntimeForProviders creates a runtime configured for every supplied tau
// provider. Registering all usable providers in one runtime lets the dynamic
// streamer and the model refresher resolve any of them by name, which is what
// makes cross-provider model switching work within a single session. The
// provider class for each is resolved by [resolveProviderClass].
func newRuntimeForProviders(providers []tauconfig.ProviderConfig, insecure bool) *runtime.Runtime {
	runtime.RegisterBuiltinClasses()
	runtime.RegisterClass(codexClass{})

	cfgs := make(map[string]runtime.ProviderConfig, len(providers))
	for _, provider := range providers {
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
				MaxOutputTokens: firstNonZero(m.MaxTokens, m.DefaultMaxTokens),
				Reasoning:       m.Reasoning,
				Extra:           extra,
			})
		}

		class := resolveProviderClass(provider)
		cfgs[provider.Name] = runtime.ProviderConfig{
			ID:       provider.Name,
			Class:    class,
			BaseURL:  runtimeProviderBaseURL(provider, class),
			Headers:  provider.Headers,
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
		}
	}

	cfg := runtime.Config{Providers: cfgs}
	// Prefer the embedded, curated model snapshot so discovery works offline and
	// is restricted to tool-capable models from providers tau knows about. Fall
	// back to a catalogue-less runtime (callers may still LoadCatalog from the
	// network) if the embed somehow fails to load.
	if cat, err := snapshot.Catalog(); err != nil {
		slog.Warn("embedded model snapshot unavailable, falling back to network catalog", "err", err)
		return runtime.NewRuntime(cfg)
	} else {
		return runtime.NewRuntimeWithCatalog(cfg, cat)
	}
}

// newCoordinatorResult bundles a coordinator with its command registry so
// the caller can seed the TUI with the initial command snapshot before the
// bus delivers the first CommandsChangedEvent.
type newCoordinatorResult struct {
	Coordinator       *agent.Coordinator
	CommandRegistry   *commandreg.Registry
	ExtensionCommands []tauchat.ExtensionCommand
	PluginManager     *plugin.Manager
}

// newCoordinator creates and returns an agent coordinator with the standard
// tool registry, config, and session persistence.
func newCoordinator(ctx context.Context, opts ChatOptions, bearerToken string, sessionManager *sessions.Manager, startupEvents []tauchat.ChatEvent, bus *eventbus.Bus, streamer agent.Streamer, rt *runtime.Runtime, modelRefs []tauchat.ChatModelRef, skillsMgr *skills.Manager, skillsDiscoveryConfig skills.DiscoveryConfig, deferPlugins bool, agentInstanceID string) (*newCoordinatorResult, error) {
	cwd, _ := os.Getwd()
	cmdRegClient := bus.Client("command-registry")
	cmdReg := commandreg.New(cwd, cmdRegClient)
	workspaceIndex, indexErr := indexing.NewManager(ctx, cwd)
	if indexErr != nil {
		slog.Warn("workspace codesearch unavailable; grep will use direct search", "err", indexErr)
	}

	coordinator, extCmds, pluginMgr, err := buildCoordinator(ctx, coordinatorConfig{
		Bus:                   bus,
		ChatOptions:           opts,
		BearerToken:           bearerToken,
		SessionManager:        sessionManager,
		InteractiveUI:         true,
		StartupEvents:         startupEvents,
		CommandRegistry:       cmdReg,
		AutoExportJSONL:       true,
		Streamer:              streamer,
		Runtime:               rt,
		ModelRefs:             modelRefs,
		SkillsManager:         skillsMgr,
		SkillsDiscoveryConfig: skillsDiscoveryConfig,
		DeferPluginLoad:       deferPlugins,
		GrepIndex:             workspaceIndex,
		AgentInstanceID:       agentInstanceID,
	})
	if err != nil {
		cmdReg.Close()
		return nil, err
	}
	return &newCoordinatorResult{
		Coordinator:       coordinator,
		CommandRegistry:   cmdReg,
		ExtensionCommands: extCmds,
		PluginManager:     pluginMgr,
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
	// Runtime is the ai-sdk runtime used to resolve providers for the
	// "subagent" delegation tool. Nil disables that tool.
	Runtime   *runtime.Runtime
	ModelRefs []tauchat.ChatModelRef
	// SkillsManager provides the skill catalog snapshot. When nil the
	// coordinator falls back to inline discovery (headless path).
	SkillsManager *skills.Manager
	// SkillsDiscoveryConfig is the last-used config for hot-reloading skills
	// via /skills-reload.
	SkillsDiscoveryConfig skills.DiscoveryConfig
	// DeferPluginLoad skips pluginMgr.Load() so the caller can defer it
	// until after the TUI has subscribed to bus events.
	DeferPluginLoad bool
	// MetricsConfig controls observability export.
	MetricsConfig tauconfig.MetricsConfig
	// GrepIndex optionally accelerates workspace-wide grep candidate selection.
	GrepIndex tools.GrepIndex
	// MaxTurns / Timeout / MaxTokens / Deadline are structural and per-task
	// budget limits enforced by the coordinator's checkLimits. Zero means
	// unlimited.
	MaxTurns  int
	Timeout   time.Duration
	MaxTokens int
	Deadline  time.Time
	// AgentInstanceID is this process's own root agent_instances row
	// (from agent.Instantiate at startup), used as ParentInstanceID when
	// registering the agent tool so child spawns/resumes correctly
	// attribute to the root instance. Empty when instantiation was
	// skipped or failed (e.g. no session store) - the agent tool then
	// treats this coordinator as depth-0 with no instance identity.
	AgentInstanceID string
}

// buildCoordinator creates a coordinator with the full plugin/tool setup.
func buildCoordinator(ctx context.Context, cfg coordinatorConfig) (*agent.Coordinator, []tauchat.ExtensionCommand, *plugin.Manager, error) {
	cwd, _ := os.Getwd()
	log := slog.Default()
	if cfg.ChatOptions.Logger != nil {
		log = cfg.ChatOptions.Logger
	}
	// pluginMgr is assigned below, after the docs tool is registered; the
	// closure reads it lazily so the docs tool always sees the live plugin
	// manager (including across hot reloads) rather than a nil snapshot.
	var pluginMgr *plugin.Manager
	registry := tools.NewRegistry()
	if err := tools.RegisterBuiltins(registry, cwd, func() map[string]string {
		if pluginMgr == nil {
			return nil
		}
		return pluginMgr.PluginDocs()
	}, cfg.GrepIndex); err != nil {
		return nil, nil, nil, fmt.Errorf("registering built-in tools: %w", err)
	}

	// Create a skill Tracker and register the Skill tool. The allowed-tools
	// callback is wired after coordinator creation since it needs the
	// Coordinator's SetAllowedTools method.
	skillTracker := skills.NewTracker()
	var setAllowedToolsFn func([]string)
	tools.RegisterSkillTool(registry, cfg.SkillsManager, skillTracker, func(toolNames []string) {
		if setAllowedToolsFn != nil {
			setAllowedToolsFn(toolNames)
		}
	})

	// Notifications pushed by plugins via HostService.Notify surface as chat
	// notifications in every connected client (TUI + web).
	pluginHostClient := cfg.Bus.Client("plugin-host")
	notifyPub := eventbus.Publish[tauchat.ChatEvent](pluginHostClient)
	pluginNotify := func(level, message string) {
		lvl := tauchat.ChatNotificationInfo
		switch level {
		case "warn":
			lvl = tauchat.ChatNotificationWarn
		case "error":
			lvl = tauchat.ChatNotificationError
		}
		notifyPub.Publish(tauchat.ChatNotificationEvent{
			Message:    message,
			Level:      lvl,
			OccurredAt: time.Now().UTC(),
		})
	}

	// Plugin manager - discovers and manages extension binaries.
	pluginLogger := slog.Default()
	if cfg.ChatOptions.Logger != nil {
		pluginLogger = cfg.ChatOptions.Logger
	}
	pluginLogger = pluginLogger.With("component", "plugin.manager")

	pluginMgr, err := plugin.NewManager(plugin.Config{
		ToolRegistry: registry,
		Logger:       pluginLogger,
		// go-plugin's own hclog output goes to the tau log file, never stderr:
		// stderr is the terminal the TUI is drawing on.
		LogOutput: taulogger.Sink(),
		Plugins:   cfg.ChatOptions.Config.Plugins,
		Notify:    pluginNotify,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("plugin manager: %w", err)
	}

	registry.SetPluginToolExecutor(func(ctx context.Context, pluginName, toolName string, args json.RawMessage) (tools.Result, error) {
		return pluginMgr.ExecutePluginTool(ctx, pluginName, toolName, args)
	})

	// loadPlugins starts plugin binaries, publishes extension commands,
	// and notifies the user. Extracted so interactive callers can defer
	// this step until after the TUI subscribes to bus events.
	loadPlugins := func() {
		if err := pluginMgr.Load(ctx); err != nil {
			log.Warn("plugin manager load failed", "err", err)
			notifyPub.Publish(tauchat.ChatNotificationEvent{
				Message:    "Plugin load failed: " + err.Error(),
				Level:      tauchat.ChatNotificationError,
				OccurredAt: time.Now().UTC(),
			})
			return
		}
		extCmds := pluginMgr.ExtensionCommands()
		if len(extCmds) > 0 {
			notifyPub.Publish(tauchat.ExtensionCommandsChangedEvent{
				Commands:   extCmds,
				OccurredAt: time.Now().UTC(),
			})
		}
		loadedMsg := pluginLoadMessage(pluginMgr)
		if loadedMsg != "" {
			notifyPub.Publish(tauchat.ChatNotificationEvent{
				Message:    loadedMsg,
				Level:      tauchat.ChatNotificationInfo,
				OccurredAt: time.Now().UTC(),
			})
		}
	}

	if cfg.DeferPluginLoad {
		log.Debug("plugin loading deferred until after TUI subscription")
	} else {
		loadPlugins()
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

	// Plugin hot reload: poll for sentinel file written by CLI install/uninstall.
	// Runs independently of the schedule tick so plugin changes are picked up
	// even when TAU_SCHEDULE_INTERVAL is not set.
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		sentinelPath := filepath.Join(tauconfig.Dir(), plugin.SentinelFile)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := os.Stat(sentinelPath); err == nil {
					if _, reloadErr := pluginMgr.ReloadExtensions(ctx, false); reloadErr != nil {
						slog.Warn("plugin reload failed", "error", reloadErr)
					}
					if err := os.Remove(sentinelPath); err != nil {
						slog.Warn("failed to remove plugin sentinel", "error", err)
					}
				}
			}
		}
	}()

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

	// Command registry - the caller creates this and passes it in so the
	// initial command snapshot can be seeded into the TUI before the bus
	// delivers the first CommandsChangedEvent.
	if cfg.CommandRegistry != nil {
		cfg.CommandRegistry.Discover()
		// Discover skills once and share with the registry to avoid
		// redundant filesystem walks (the prompt builder also uses
		// skill discovery).
		var allSkills []*skills.Skill
		if cfg.SkillsManager != nil {
			allSkills = cfg.SkillsManager.Snapshot().AllSkills
		} else {
			allSkills, _ = skills.Discover(skills.DefaultSources(cwd))
		}
		cfg.CommandRegistry.MergeSkills(allSkills)
	}

	coordinator, err := agent.NewCoordinator(ctx, agent.CoordinatorConfig{
		Bus:                   cfg.Bus,
		Logger:                cfg.ChatOptions.Logger,
		TokenSource:           staticTokenSource(cfg.BearerToken),
		Streamer:              cfg.Streamer,
		Registry:              registry,
		InteractiveUI:         cfg.InteractiveUI,
		SessionManager:        cfg.SessionManager,
		NoPersist:             cfg.ChatOptions.Ephemeral,
		AllowedTools:          cfg.ChatOptions.AllowedTools,
		AutoExportJSONL:       cfg.AutoExportJSONL,
		StartupEvents:         cfg.StartupEvents,
		ProjectDir:            cwd,
		SkillTracker:          skillTracker,
		SkillsManager:         cfg.SkillsManager,
		CommandRegistry:       cfg.CommandRegistry,
		SkillsDiscoveryConfig: cfg.SkillsDiscoveryConfig,
		ExtensionReloader:     pluginMgr,
		OnPluginEvent: func(event, sessionID string, payload *api.EventPayload) *api.EventResponse {
			return pluginMgr.DispatchEvent(ctx, event, sessionID, payload)
		},
		OnClose: func() {
			pluginMgr.Unload(context.Background())
			pluginBusClient.Close()
			pluginHostClient.Close()
		},
		ModelLookup: func(modelID string) *tauchat.ChatModelRef {
			for i, ref := range cfg.ModelRefs {
				if ref.ID == modelID {
					return &cfg.ModelRefs[i]
				}
			}
			return nil
		},
		SubagentExecutor: subagentExecutorOrNil(cfg.Runtime, cwd),
		MetricsConfig:    cfg.MetricsConfig,
		MaxTurns:         cfg.MaxTurns,
		Timeout:          cfg.Timeout,
		MaxTokens:        cfg.MaxTokens,
		Deadline:         cfg.Deadline,
		AutoCompact:      cfg.ChatOptions.Config.AutoCompact,
	})
	if err != nil {
		return nil, nil, nil, err
	}

	// Wire up the allowed-tools callback now that the coordinator exists.
	setAllowedToolsFn = coordinator.SetAllowedTools

	// Wire the coordinator's interactive UI bridge to the plugin host so
	// plugin slash commands can call Host.Confirm / Host.Input.
	pluginMgr.SetInteractiveHandler(coordinator.UIBridge())

	// The same bridge also implements plugin.ViewRenderer (RenderView/
	// CloseView), but tools.UIBridge - the interface Coordinator.UIBridge()
	// is statically typed as - doesn't declare those methods, so a type
	// assertion is needed to recover them.
	if viewRenderer, ok := coordinator.UIBridge().(plugin.ViewRenderer); ok {
		pluginMgr.SetViewRenderer(viewRenderer)
	}

	// Register the agent tool for spawning child processes. Store/Bus/
	// ParentInstanceID are this coordinator's own identity (it's always
	// the root - depth 0, no parent ceiling); SessionID is deliberately
	// left empty here since it's per-call, not per-coordinator - see
	// executeAgentTool, which reads it from the UIBridge instead.
	var agentStore store.SessionStore
	if cfg.SessionManager != nil {
		agentStore = cfg.SessionManager.Store()
	}
	tauPath, err := os.Executable()
	if err != nil {
		tauPath = "" // falls back to PATH lookup in the agent tool
	}
	agentCfg := tools.AgentToolConfig{
		CWD:                  cwd,
		Store:                agentStore,
		ModelModes:           cfg.ChatOptions.Config.ModelModes,
		DefaultProvider:      cfg.ChatOptions.Config.DefaultProvider,
		DefaultModel:         cfg.ChatOptions.Config.DefaultModel,
		Agents:               cfg.ChatOptions.Config.Agents,
		ParentInstanceID:     cfg.AgentInstanceID,
		ParentDepth:          0,
		ParentEffectiveTools: cfg.ChatOptions.AllowedTools,
		InheritedProvider:    cfg.ChatOptions.Provider.Name,
		InheritedModel:       cfg.ChatOptions.Model,
		TauPath:              tauPath,
		Bus:                  cfg.Bus,
	}
	if err := registry.Register(tools.NewAgentTool(agentCfg)); err != nil {
		return nil, nil, nil, fmt.Errorf("registering agent tool: %w", err)
	}

	return coordinator, pluginMgr.ExtensionCommands(), pluginMgr, nil
}

// pluginLoadMessage builds a human-readable summary of loaded plugins
// for the startup notification. Returns empty string when no plugins loaded.
func pluginLoadMessage(mgr *plugin.Manager) string {
	extCmds := mgr.ExtensionCommands()
	if len(extCmds) == 0 {
		return ""
	}
	// Count unique plugin names from command ExtensionName fields.
	plugins := make(map[string]int) // plugin name → command count
	for _, cmd := range extCmds {
		plugins[cmd.ExtensionName]++
	}
	var parts []string
	for name, count := range plugins {
		if count == 1 {
			parts = append(parts, name+" (1 command)")
		} else {
			parts = append(parts, fmt.Sprintf("%s (%d commands)", name, count))
		}
	}
	sort.Strings(parts)
	return "Plugins loaded: " + strings.Join(parts, ", ")
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
