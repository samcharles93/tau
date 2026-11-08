package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	aisdkruntime "github.com/samcharles93/ai-sdk/runtime"

	"github.com/samcharles93/tau/internal/agent"
	"github.com/samcharles93/tau/internal/agent/tools"
	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	commandreg "github.com/samcharles93/tau/internal/registry"
	"github.com/samcharles93/tau/internal/sessions"
	"github.com/samcharles93/tau/internal/skills"
	"github.com/samcharles93/tau/internal/store"
	"github.com/samcharles93/tau/internal/tui"
)

// startupLog is a dedicated file logger for startup diagnostics.
// slog output goes to stderr which would corrupt the TUI, so we write
// diagnostics to a separate file under the tau config directory.
var startupLog *slog.Logger

func initStartupLog() {
	if startupLog != nil {
		return
	}
	logDir := filepath.Join(tauconfig.Dir(), "logs")
	_ = os.MkdirAll(logDir, 0o700)
	f, err := os.OpenFile(filepath.Join(logDir, "startup.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		startupLog = slog.New(slog.DiscardHandler)
		return
	}
	startupLog = slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// logStartupPhase logs memory stats and elapsed time for a startup phase.
// Writes to ~/.tau/logs/startup.log so the TUI is not corrupted.
func logStartupPhase(phase string, t0 time.Time) {
	if startupLog == nil {
		initStartupLog()
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	elapsed := time.Since(t0)
	startupLog.Info(
		"startup phase",
		"phase", phase,
		"elapsed_ms", elapsed.Milliseconds(),
		"heap_mb", m.Alloc/1024/1024,
		"total_mb", m.Sys/1024/1024,
		"num_gc", m.NumGC,
	)
}

// RunChat orchestrates an interactive chat session: resolves tokens and model,
// creates the coordinator, then launches the TUI.
func RunChat(ctx context.Context, opts ChatOptions) error {
	t0 := time.Now()
	logStartupPhase("start", t0)

	cr, err := setupChatRuntime(opts, t0)
	if err != nil {
		return err
	}
	rt, pr, allModels, model, streamer, discoverErr := cr.rt, cr.pr, cr.models, cr.model, cr.streamer, cr.discoverErr
	usableProviders := opts.Config.Providers

	// Initialize session store at the RunChat level so it outlives the
	// coordinator (needed for --resume and exit summary).
	rawStore, sessionManager, storeErr := openSessionManager(ctx)
	if storeErr != nil {
		slog.Warn("session store unavailable, sessions will not be persisted", "err", storeErr)
	}
	logStartupPhase("after-store", t0)

	// Create the central event bus. All subsystems (coordinator, TUI, skills)
	// communicate through this bus as named clients. The bus enforces total
	// ordering of published events and typed routing via Go generics.
	bus := eventbus.New()
	defer bus.Close()
	logStartupPhase("after-bus", t0)
	cwd, _ := os.Getwd()
	skillsMgr, skillDiscoveryCfg, systemPrompt := initSkillsAndPrompt(ctx, bus, opts, cwd)
	defer skillsMgr.Close()

	// Build the available model refs early so the coordinator can use them
	// for model metadata lookups (context window, pricing). Aggregate across
	// every usable provider so /model can switch providers live.
	available := aggregateModelRefs(ctx, rt, opts.Insecure, usableProviders)

	startupEvents := collectStartupEvents(discoverErr, model, available)

	// Instantiate the root agent identity. This creates the agent_instances
	// row and resolves the spec, model, and effective tools.
	agentInstanceID, agentEvents, err := instantiateRootAgent(ctx, opts, sessionManager, rawStore, cwd)
	if err != nil {
		return err
	}
	startupEvents = append(startupEvents, agentEvents...)

	// Auth is resolved by the runtime when the provider is created.
	// Auth is resolved by the runtime when the provider is created.
	// The coordinator only needs a token source for legacy compatibility;
	// pass an empty token.
	result, coordinator, err := createChatCoordinator(ctx, opts, sessionManager, startupEvents, bus, streamer, rt, available, skillsMgr, skillDiscoveryCfg, agentInstanceID, t0)
	if err != nil {
		return err
	}
	defer coordinator.Close()
	defer result.CommandRegistry.Close()
	logStartupPhase("after-coordinator", t0)

	// OnReady: after the TUI subscribes to bus events, load plugins so the
	// initial ExtensionCommandsChangedEvent reaches every client (TUI, web).
	// This is a bus publish so subscribers receive it - the coordinator's Send
	// expects ChatCommand, not ChatEvent.
	eventPub := eventbus.Publish[tauchat.ChatEvent](bus.Client("plugin-onready"))
	pluginOnReady := newPluginOnReady(ctx, bus, result)

	sessionID, err := tauchat.NewID()
	if err != nil {
		return err
	}

	// StartChatSessionCommand/LoadSessionCommand are sent from chatSessionOnReady
	// (below, run via TUIConfig.OnReady) rather than eagerly here - Coordinator.Send
	// just enqueues onto an internal channel processed by a background goroutine, so
	// sending immediately races the TUI's own bus subscription (which happens inside
	// tui.Run, called well after this point). On --resume this race is real and
	// visible: SessionLoadedEvent can be published and dropped before anything is
	// listening, so the loaded history never renders until the next unrelated
	// snapshot (e.g. after the user's first submitted prompt) rebuilds it. Deferring
	// to OnReady mirrors pluginOnReady's identical fix for the identical race.
	//
	// Because OnReady has no error return, a failed Send() here can no longer abort
	// startup the way an eager Send() could - surfaced as a notification instead,
	// same as pluginOnReady's own failure handling below.
	resumePlan, err := makeSessionOnReady(ctx, opts, sessionManager, allModels, systemPrompt, coordinator, eventPub, sessionID, model)
	if err != nil {
		return err
	}
	sessionID, model, chatSessionOnReady, resumeSummary := resumePlan.sessionID, resumePlan.model, resumePlan.onReady, resumePlan.summary

	// Build a refresher closure that the TUI can use to re-discover models.
	refresher := buildModelRefresher(pr)

	// Command registry owns command state; TUI initialises from snapshot
	// and receives deltas via CommandsChangedEvent on the bus.
	initialCommands := commandRefsFromRegistry(result.CommandRegistry.All())

	// restartWanted is set by /update once a new binary is in place; the re-exec
	// happens after the TUI exits (see the end of this function).
	var restartWanted atomic.Bool

	// Start the chat session (and load its history, on --resume) before
	// loading plugins - both run only once the TUI has subscribed to bus
	// events (see chatSessionOnReady's own comment for why).
	onReady := func() {
		chatSessionOnReady()
		if pluginOnReady != nil {
			pluginOnReady()
		}
		go StartBackgroundUpdateCheck(ctx, opts.Version, opts.Config.Updates, bus)
	}

	tuiCfg := buildTUIConfig(opts, sessionID, model, available, initialCommands, bus, refresher, &restartWanted, onReady)

	logStartupPhase("after-tui", t0)
	tuiErr := tui.Run(ctx, coordinator, tuiCfg)
	return finishChatRun(ctx, coordinator, opts, sessionManager, sessionID, resumeSummary, restartWanted.Load(), tuiErr)
}

// commandRefsFromRegistry filters registry commands to the subset the TUI
// needs (built-in + custom; skill commands use the /skill: prefix and are
// included as-is from MergeSkills).
func commandRefsFromRegistry(cmds []commandreg.Command) []tauchat.CommandRef {
	refs := make([]tauchat.CommandRef, 0, len(cmds))
	for _, c := range cmds {
		refs = append(refs, tauchat.CommandRef{
			Name:        c.Name,
			Label:       c.Label,
			Description: c.Description,
			AcceptsArgs: c.AcceptsArgs,
		})
	}
	return refs
}

// chatRuntime bundles the per-launch provider runtime, discovered models,
// selected model, and the dynamic streamer built from them.
type chatRuntime struct {
	rt          *aisdkruntime.Runtime
	pr          *providerRuntime
	models      []aisdkruntime.ModelInfo
	model       tauchat.ChatModelRef
	streamer    agent.Streamer
	discoverErr error
}

// setupChatRuntime builds the multi-provider runtime, discovers models from
// the loaded catalog, picks the launch model, and creates the dynamic
// streamer. Model discovery failures are captured in discoverErr (surfaced as
// a startup notification) rather than aborting startup.
func setupChatRuntime(opts ChatOptions, t0 time.Time) (chatRuntime, error) {
	var cr chatRuntime
	// Build a runtime holding every usable provider so the dynamic streamer
	// and refresher can resolve any of them by name, enabling cross-provider
	// model switching within a single session.
	usableProviders := opts.Config.Providers
	cr.rt = newRuntimeForProviders(usableProviders, opts.Insecure)
	// Wrap the runtime so it can be rebuilt live when provider state changes
	// (e.g. after /provider), keeping the streamer and refresher in sync.
	cr.pr = newProviderRuntime(cr.rt, usableProviders, opts.Insecure)
	logStartupPhase("after-config", t0)

	// Models come from the embedded snapshot loaded in newRuntimeForProviders,
	// so discovery needs no network.
	cr.models, cr.discoverErr = cr.rt.Models(opts.Provider.Name)

	var err error
	cr.model, err = pickModel(cr.models, opts.Model, opts.Config.DefaultModel, opts.Provider.Name, opts.Provider.BaseURL)
	if err != nil {
		return cr, err
	}

	// One dynamic streamer serves every provider; it picks the provider/model
	// per turn from the session state.
	cr.streamer = buildDynamicStreamer(cr.pr)
	return cr, nil
}

// instantiateRootAgent sweeps orphaned agent instances and creates this
// process's root agent identity when a session store is available. It returns
// the new instance ID plus any startup notifications to surface in the TUI.
func instantiateRootAgent(ctx context.Context, opts ChatOptions, sessionManager *sessions.Manager, rawStore store.SessionStore, cwd string) (string, []tauchat.ChatEvent, error) {
	var startupEvents []tauchat.ChatEvent
	if sessionManager == nil {
		return "", nil, nil
	}
	var agentInstanceID string

	// Orphan sweep runs once at root startup, before this process adds
	// its own row, so any instance left "started" by a prior crashed
	// process gets closed rather than accumulating forever. Best
	// effort: a sweep failure must not block startup.
	if err := agent.SweepOrphanedInstances(ctx, rawStore, os.Getpid(), opts.Config.Agents.OrphanStaleAge); err != nil {
		slog.Warn("orphan sweep failed", "err", err)
	}

	instCfg := agent.InstantiateConfig{
		Name:              opts.AgentSpec,
		CWD:               cwd,
		ModelOverride:     opts.Model,
		InheritedProvider: opts.Provider.Name,
		InheritedModel:    opts.Model,
		ModelModes:        opts.Config.ModelModes,
		DefaultProvider:   opts.Config.DefaultProvider,
		DefaultModel:      opts.Config.DefaultModel,
		Agents:            opts.Config.Agents,
		Store:             rawStore,
	}

	// Root-spec override trust gate (G14): only the default bare "tau"
	// resolution path is subject to implicit project/user discovery -
	// an explicit --agent <other-name> is a deliberate user choice, not
	// a silent override, and is out of scope for this gate. See
	// docs/specs/agents/01-agent-spec-format.md (Root-spec override
	// trust).
	var rootDisplay *rootSpecDisplay
	if strings.TrimSpace(opts.AgentSpec) == "tau" || opts.AgentSpec == "" {
		def, disp, trustErr := resolveRootSpecWithTrust(cwd, opts)
		if trustErr != nil {
			return "", nil, trustErr
		}
		instCfg.PreResolvedRootDef = def
		rootDisplay = &disp
	}

	instResult, instErr := agent.Instantiate(ctx, instCfg)
	if instErr != nil {
		slog.Warn("agent instantiation failed", "err", instErr)
	} else {
		agentInstanceID = instResult.InstanceID
		slog.Info("root agent instantiated", "instance_id", agentInstanceID,
			"provider", instResult.ResolvedProvider, "model", instResult.ResolvedModel,
			"depth", instResult.Depth)
		startupEvents = append(startupEvents, tauchat.ChatNotificationEvent{
			Message:    fmt.Sprintf("Agent instantiated: %s", agentInstanceID),
			Level:      tauchat.ChatNotificationInfo,
			OccurredAt: time.Now().UTC(),
		})

		// Display the resolved root spec whenever it came from a
		// non-built-in source, per docs/specs/agents/
		// 01-agent-spec-format.md ("Display before execution": visible
		// throughout the session, not just at startup).
		if rootDisplay != nil && rootDisplay.Scope != "builtin" {
			msg := fmt.Sprintf("Resolved root spec: %s:%s", rootDisplay.Scope, rootDisplay.Source)
			if rootDisplay.Hash != "" {
				msg += fmt.Sprintf(" (sha256:%s, trusted)", rootDisplay.Hash[:8])
			}
			slog.Info("root spec resolved", "scope", rootDisplay.Scope, "source", rootDisplay.Source, "hash", rootDisplay.Hash)
			startupEvents = append(startupEvents, tauchat.ChatNotificationEvent{
				Message:    msg,
				Level:      tauchat.ChatNotificationInfo,
				OccurredAt: time.Now().UTC(),
			})
		}
	}

	return agentInstanceID, startupEvents, nil
}

// newPluginOnReady returns the OnReady hook that loads plugin binaries and
// publishes their extension commands once the TUI has subscribed to bus
// events (the coordinator's Send expects ChatCommand, not ChatEvent, so this
// is a bus publish). Returns nil when there is no plugin manager.
func newPluginOnReady(ctx context.Context, bus *eventbus.Bus, result *newCoordinatorResult) func() {
	if result.PluginManager == nil {
		return nil
	}
	eventPub := eventbus.Publish[tauchat.ChatEvent](bus.Client("plugin-onready"))
	pluginMgr := result.PluginManager
	return func() {
		if err := pluginMgr.Load(ctx); err != nil {
			slog.Warn("plugin manager load failed", "err", err)
			eventPub.Publish(tauchat.ChatNotificationEvent{
				Message:    "Plugin load failed: " + err.Error(),
				Level:      tauchat.ChatNotificationInfo,
				OccurredAt: time.Now().UTC(),
			})
			return
		}
		extCmds := pluginMgr.ExtensionCommands(ctx)
		if len(extCmds) > 0 {
			eventPub.Publish(tauchat.ExtensionCommandsChangedEvent{
				Commands:   extCmds,
				OccurredAt: time.Now().UTC(),
			})
		}
		loadedMsg := pluginLoadMessage(ctx, pluginMgr)
		if loadedMsg != "" {
			eventPub.Publish(tauchat.ChatNotificationEvent{
				Message:    loadedMsg,
				Level:      tauchat.ChatNotificationInfo,
				OccurredAt: time.Now().UTC(),
			})
		}
	}
}

// sessionStartPlan captures how RunChat starts (or resumes) the chat session
// once the TUI has subscribed to bus events.
type sessionStartPlan struct {
	sessionID string
	model     tauchat.ChatModelRef
	onReady   func()
	summary   string
}

// startSessionOnReady returns the OnReady hook that sends
// StartChatSessionCommand (and LoadSessionCommand on resume) to the
// coordinator. Sends are deferred to OnReady because Coordinator.Send only
// enqueues - sending eagerly races the TUI's bus subscription, so
// SessionLoadedEvent could be published before anything listens and the
// loaded history would never render until the next snapshot.
func startSessionOnReady(coordinator *agent.Coordinator, eventPub *eventbus.Publisher[tauchat.ChatEvent], sessionID string, config tauchat.ChatSessionConfig, loadHistory bool) func() {
	return func() {
		if err := coordinator.Send(tauchat.StartChatSessionCommand{SessionID: sessionID, Config: config}); err != nil {
			slog.Warn("start chat session failed", "err", err)
			eventPub.Publish(tauchat.ChatNotificationEvent{
				Message:    "Failed to start session: " + err.Error(),
				Level:      tauchat.ChatNotificationError,
				OccurredAt: time.Now().UTC(),
			})
			return
		}
		if !loadHistory {
			return
		}
		// Load the message history into the running session.
		if err := coordinator.Send(tauchat.LoadSessionCommand{SessionID: sessionID}); err != nil {
			slog.Warn("load session failed", "err", err)
			eventPub.Publish(tauchat.ChatNotificationEvent{
				Message:    "Failed to load session: " + err.Error(),
				Level:      tauchat.ChatNotificationError,
				OccurredAt: time.Now().UTC(),
			})
		}
	}
}

// makeSessionOnReady builds the session start plan. On --resume it resolves
// "latest", loads the saved session, rehydrates its model through discovery
// (stored sessions persist only the model ID), and returns a summary for the
// exit message.
func makeSessionOnReady(ctx context.Context, opts ChatOptions, sessionManager *sessions.Manager, allModels []aisdkruntime.ModelInfo, systemPrompt string, coordinator *agent.Coordinator, eventPub *eventbus.Publisher[tauchat.ChatEvent], sessionID string, model tauchat.ChatModelRef) (sessionStartPlan, error) {
	plan := sessionStartPlan{sessionID: sessionID, model: model}
	if opts.ResumeSessionID == "" || sessionManager == nil {
		config := buildSessionConfig(opts, model, systemPrompt)
		plan.onReady = startSessionOnReady(coordinator, eventPub, sessionID, config, false)
		return plan, nil
	}

	resumeID := opts.ResumeSessionID
	if resumeID == "latest" {
		summaries, _, lErr := sessionManager.List(ctx, 1, "")
		if lErr != nil || len(summaries) == 0 {
			return plan, fmt.Errorf("no saved sessions to resume")
		}
		resumeID = summaries[0].ID
	}
	// No RuntimeSessionConfig - there is no live template session
	// to merge runtime config from before the coordinator starts.
	loaded, lErr := sessionManager.Load(ctx, resumeID, nil)
	if lErr != nil {
		return plan, fmt.Errorf("resume session %q: %w", resumeID, lErr)
	}
	plan.sessionID = loaded.SessionID
	// Use the loaded session's model if available, but rehydrate it through
	// discovery/fallback so the runtime has the URL and model config. Stored
	// sessions only persist the model ID.
	if loaded.Model.ID != "" {
		loadedModel, pickErr := pickModel(allModels, loaded.Model.ID, "", opts.Provider.Name, opts.Provider.BaseURL)
		if pickErr != nil {
			return plan, fmt.Errorf("resume session %q model %q: %w", resumeID, loaded.Model.ID, pickErr)
		}
		plan.model = loadedModel
	}
	config := buildSessionConfig(opts, plan.model, systemPrompt)
	config.SystemPrompt = loaded.SystemPrompt
	plan.onReady = startSessionOnReady(coordinator, eventPub, plan.sessionID, config, true)
	plan.summary = fmt.Sprintf("Session %s resumed (%d messages). Exit: save + resume with: tau --resume %s",
		plan.sessionID, len(loaded.Messages), plan.sessionID)
	return plan, nil
}

// initSkillsAndPrompt creates the skill manager on the bus, refreshes the
// catalog once at startup (disabled skills come from config; CLI --skill-dir
// flags are merged with config SkillPaths), and builds the full system prompt
// (project context + skill catalog). Built-in tools are registered on a
// throwaway registry to extract their schemas for the prompt; the real
// registry in buildCoordinator registers them again (idempotent).
func initSkillsAndPrompt(ctx context.Context, bus *eventbus.Bus, opts ChatOptions, cwd string) (*skills.Manager, skills.DiscoveryConfig, string) {
	// Skills manager - centralises discovery and publishes events on the bus
	// so subscribers (TUI, bridge) stay in sync when the catalog changes.
	skillsMgr := skills.NewManager(bus)

	extraPaths := append([]string{}, opts.Config.SkillPaths...)
	extraPaths = append(extraPaths, opts.SkillDirs...)
	skillDiscoveryCfg := skills.DiscoveryConfig{
		WorkingDir:     cwd,
		ExtraPaths:     extraPaths,
		DisabledSkills: opts.Config.DisabledSkills,
	}
	if _, err := skillsMgr.Refresh(skillDiscoveryCfg); err != nil {
		slog.Warn("skill discovery failed", "err", err)
	}
	logStartupPhase("after-skills", time.Now())

	tmpReg := tools.NewRegistry()
	var toolSchemas []tools.Schema
	if err := tools.RegisterBuiltins(tmpReg, cwd, nil); err != nil {
		slog.Warn("registering built-in tools for prompt", "err", err)
	} else {
		toolSchemas = tmpReg.Schemas()
	}
	systemPrompt := buildAgentSystemPrompt(cwd, skillsMgr, toolSchemas)
	logStartupPhase("after-prompt", time.Now())
	return skillsMgr, skillDiscoveryCfg, systemPrompt
}

// collectStartupEvents builds the startup notifications for the coordinator
// event stream so the TUI receives them on first subscribe. Model discovery
// failures are surfaced here rather than through a separate pubsub bus.
func collectStartupEvents(discoverErr error, model tauchat.ChatModelRef, available []tauchat.ChatModelRef) []tauchat.ChatEvent {
	var startupEvents []tauchat.ChatEvent
	if discoverErr != nil {
		startupEvents = append(startupEvents, tauchat.ChatNotificationEvent{
			Message:    "Model discovery failed: " + discoverErr.Error() + " (`/refresh` to retry)",
			Level:      tauchat.ChatNotificationWarn,
			OccurredAt: time.Now().UTC(),
		})
	}

	// When no model is selected at launch (no --model, no default_model), the
	// session starts unselected. Point the user at /model (or /provider if
	// there is nothing to choose from yet).
	if strings.TrimSpace(model.ID) == "" {
		hint := "No model selected - use /model to choose one."
		if len(available) == 0 {
			hint = "No models available - use /provider to enable a provider."
		}
		startupEvents = append(startupEvents, tauchat.ChatNotificationEvent{
			Message:    hint,
			Level:      tauchat.ChatNotificationInfo,
			OccurredAt: time.Now().UTC(),
		})
	}
	return startupEvents
}

// createChatCoordinator builds the coordinator and registers the close defers.
// On failure it closes the session manager so no store handle leaks.
func createChatCoordinator(ctx context.Context, opts ChatOptions, sessionManager *sessions.Manager, startupEvents []tauchat.ChatEvent, bus *eventbus.Bus, streamer agent.Streamer, rt *aisdkruntime.Runtime, available []tauchat.ChatModelRef, skillsMgr *skills.Manager, skillDiscoveryCfg skills.DiscoveryConfig, agentInstanceID string, t0 time.Time) (*newCoordinatorResult, *agent.Coordinator, error) {
	result, err := newCoordinator(ctx, opts, "", sessionManager, startupEvents, bus, streamer, rt, available, skillsMgr, skillDiscoveryCfg, true, agentInstanceID)
	if err != nil {
		if sessionManager != nil {
			if err := sessionManager.Close(); err != nil {
				slog.Warn("closing session store", "err", err)
			}
		}
		return nil, nil, err
	}
	coordinator := result.Coordinator
	// Defers run in RunChat's scope via the returned closures.
	return result, coordinator, nil
}

// finishChatRun closes the coordinator (so it can persist sessions while the
// store is still open), decides the /update re-exec argv, prints the exit
// summary, closes the store, and performs the re-exec if an update was applied.
func finishChatRun(ctx context.Context, coordinator *agent.Coordinator, opts ChatOptions, sessionManager *sessions.Manager, sessionID, resumeSummary string, restartWanted bool, tuiErr error) error {
	coordinator.Close()

	// Decide the re-exec argv while the store is still open: /update's restart
	// should resume the session that was just persisted, but empty sessions are
	// deliberately never persisted, and ephemeral sessions neither - resuming
	// a missing row would make the restarted process fail at startup.
	var restartExtraArgs []string
	if restartWanted && tuiErr == nil && !opts.Ephemeral && sessionManager != nil {
		// Deliberately not ctx: this runs on the shutdown path where ctx may
		// already be cancelled (same rationale as persistSession).
		probeCtx, probeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		//nolint:contextcheck // shutdown-path probe, ctx may already be done
		if _, err := sessionManager.Load(probeCtx, sessionID, nil); err == nil {
			restartExtraArgs = resumeRestartArgs(sessionID)
		}
		probeCancel()
	}

	// Print session summary on exit.
	if sessionManager != nil {
		printExitSummary(ctx, sessionManager, sessionID, resumeSummary)
		if err := sessionManager.Close(); err != nil {
			slog.Warn("closing session store", "err", err)
		}
	}

	// /update replaced this binary on disk: re-exec so the session the user
	// gets back is running the new code. Done last, after the TUI has restored
	// the terminal and every resource above is released. The --resume pair in
	// restartExtraArgs lands the restarted process back in the same
	// conversation instead of a fresh one.
	if restartWanted && tuiErr == nil {
		if err := restartInPlace(restartExtraArgs); err != nil {
			fmt.Fprintf(os.Stderr, "tau updated but could not restart: %v\nrun tau again to use the new version\n", err)
		}
	}

	return tuiErr
}

// buildTUIConfig assembles the TUI configuration for an interactive run.
func buildTUIConfig(opts ChatOptions, sessionID string, model tauchat.ChatModelRef, available []tauchat.ChatModelRef, initialCommands []tauchat.CommandRef, bus *eventbus.Bus, refresher tui.ModelRefresher, restartWanted *atomic.Bool, onReady func()) tui.TUIConfig {
	return tui.TUIConfig{
		SessionID:                 sessionID,
		ModelName:                 model.ID,
		Provider:                  opts.Provider.Name,
		AvailableModels:           available,
		AvailableProviders:        tauconfig.ProviderNames(opts.Config),
		InitialCommands:           initialCommands,
		Bus:                       bus,
		RefreshModels:             refresher,
		UpdateFunc:                NewUpdateFunc(opts.Version, opts.Config.Updates, restartWanted),
		ShowReasoning:             opts.Config.UI.ReasoningMode() != tauconfig.ReasoningOff,
		ReasoningMode:             opts.Config.UI.ReasoningMode(),
		ReasoningEffort:           opts.ReasoningEffort,
		Debug:                     isDevel(opts.Version, opts.Config),
		OnReady:                   onReady,
		MetricsConfig:             opts.Config.Metrics,
		ToolCallsDefaultCollapsed: opts.Config.UI.ToolCallsDefaultCollapsed,
		NewTUI:                    opts.NewTUI,
	}
}
