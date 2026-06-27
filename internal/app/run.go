package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	commandreg "github.com/samcharles93/tau/internal/registry"
	"github.com/samcharles93/tau/internal/sessions"
	"github.com/samcharles93/tau/internal/skills"
	"github.com/samcharles93/tau/internal/tui"
)

// RunChat orchestrates an interactive chat session: resolves tokens and model,
// creates the coordinator, then launches the TUI.
func RunChat(ctx context.Context, opts ChatOptions) error {
	// Build a runtime holding every usable provider so the dynamic streamer
	// and refresher can resolve any of them by name, enabling cross-provider
	// model switching within a single session.
	usableProviders := opts.Config.Providers
	rt := newRuntimeForProviders(usableProviders, opts.Insecure)
	// Wrap the runtime so it can be rebuilt live when provider state changes
	// (e.g. after /login), keeping the streamer and refresher in sync.
	pr := newProviderRuntime(rt, usableProviders, opts.Insecure)

	// Models come from the embedded snapshot loaded in newRuntimeForProviders,
	// so discovery needs no network.
	var discoverErr error
	allModels, modelsErr := rt.Models(opts.Provider.Name)
	if modelsErr != nil {
		discoverErr = modelsErr
	}

	model, err := pickModel(allModels, opts.Model, opts.Config.DefaultModel, opts.Provider.Name, opts.Provider.BaseURL)
	if err != nil {
		return err
	}

	// One dynamic streamer serves every provider; it picks the provider/model
	// per turn from the session state.
	streamer := buildDynamicStreamer(pr)

	// Initialize session store at the RunChat level so it outlives the
	// coordinator (needed for --resume and exit summary).
	rawStore, storeErr := sessions.OpenStore()
	var sessionManager *sessions.Manager
	if storeErr != nil {
		slog.Warn("session store unavailable, sessions will not be persisted", "err", storeErr)
	} else {
		sessionManager = sessions.NewManager(rawStore)
	}

	// Create the central event bus. All subsystems (coordinator, TUI, skills)
	// communicate through this bus as named clients. The bus enforces total
	// ordering of published events and typed routing via Go generics.
	bus := eventbus.New()
	defer bus.Close()

	// Skills manager — centralises discovery and publishes events on the bus
	// so subscribers (TUI, bridge) stay in sync when the catalog changes.
	skillsMgr := skills.NewManager(bus)
	defer skillsMgr.Close()

	cwd, _ := os.Getwd()
	// Refresh the skill catalog once at startup. Disabled skills are read from
	// config so user preferences persist across sessions. CLI --skill-dir flags
	// are merged with config SkillPaths.
	extraPaths := append([]string{}, opts.Config.SkillPaths...)
	extraPaths = append(extraPaths, opts.SkillDirs...)
	if _, err := skillsMgr.Refresh(skills.DiscoveryConfig{
		WorkingDir:     cwd,
		ExtraPaths:     extraPaths,
		DisabledSkills: opts.Config.DisabledSkills,
	}); err != nil {
		slog.Warn("skill discovery failed", "err", err)
	}

	// Build the full system prompt — project context (AGENTS.md) + skill catalog.
	systemPrompt := buildAgentSystemPrompt("", cwd, skillsMgr) // Left user prompt there in case we want to expand this later.

	// Collect startup notifications for the coordinator event stream so the
	// TUI receives them on first subscribe. Model discovery failures are
	// surfaced here rather than through a separate pubsub bus.
	var startupEvents []tauchat.ChatEvent
	if discoverErr != nil {
		startupEvents = append(startupEvents, tauchat.ChatNotificationEvent{
			Message:    "Model discovery failed: " + discoverErr.Error() + " (`/refresh` to retry)",
			Level:      tauchat.ChatNotificationWarn,
			OccurredAt: time.Now().UTC(),
		})
	}

	// Build the available model refs early so the coordinator can use them
	// for model metadata lookups (context window, pricing). Aggregate across
	// every usable provider so /model can switch providers live.
	available := aggregateModelRefs(ctx, rt, opts.Insecure, usableProviders)

	// When no model is selected at launch (no --model, no default_model), the
	// session starts unselected. Point the user at /model (or /login if there
	// is nothing to choose from yet).
	if strings.TrimSpace(model.ID) == "" {
		hint := "No model selected — use /model to choose one."
		if len(available) == 0 {
			hint = "No models available — use /login to enable a provider."
		}
		startupEvents = append(startupEvents, tauchat.ChatNotificationEvent{
			Message:    hint,
			Level:      tauchat.ChatNotificationInfo,
			OccurredAt: time.Now().UTC(),
		})
	}

	// Auth is resolved by the runtime when the provider is created.
	// The coordinator only needs a token source for legacy compatibility;
	// pass an empty token.
	result, err := newCoordinator(ctx, opts, "", sessionManager, startupEvents, bus, streamer, available, skillsMgr)
	if err != nil {
		if sessionManager != nil {
			if err := sessionManager.Close(); err != nil {
				slog.Warn("closing session store", "err", err)
			}
		}
		return err
	}
	coordinator := result.Coordinator
	defer coordinator.Close()
	defer result.CommandRegistry.Close()

	sessionID, err := newID()
	if err != nil {
		return err
	}

	// If --resume is set, load the session and use its ID/properties.
	resumeSummary := "" // printed on exit
	if opts.ResumeSessionID != "" && sessionManager != nil {
		resumeID := opts.ResumeSessionID
		if resumeID == "latest" {
			summaries, _, lErr := sessionManager.List(ctx, 1, "")
			if lErr != nil || len(summaries) == 0 {
				return fmt.Errorf("no saved sessions to resume")
			}
			resumeID = summaries[0].ID
		}
		// No RuntimeSessionConfig — there is no live template session
		// to merge runtime config from before the coordinator starts.
		loaded, lErr := sessionManager.Load(ctx, resumeID, nil)
		if lErr != nil {
			return fmt.Errorf("resume session %q: %w", resumeID, lErr)
		}
		sessionID = loaded.SessionID
		// Use the loaded session's model if available, but rehydrate it through
		// discovery/fallback so the runtime has the URL and model config. Stored
		// sessions only persist the model ID.
		if loaded.Model.ID != "" {
			loadedModel, pickErr := pickModel(allModels, loaded.Model.ID, "", opts.Provider.Name, opts.Provider.BaseURL)
			if pickErr != nil {
				return fmt.Errorf("resume session %q model %q: %w", resumeID, loaded.Model.ID, pickErr)
			}
			model = loadedModel
		}
		// Start the session, then load the messages via LoadSessionCommand.
		config := buildSessionConfig(opts, model, systemPrompt)
		config.SystemPrompt = loaded.SystemPrompt
		if err := coordinator.Send(tauchat.StartChatSessionCommand{SessionID: sessionID, Config: config}); err != nil {
			return err
		}
		// Load the message history into the running session.
		if err := coordinator.Send(tauchat.LoadSessionCommand{SessionID: sessionID}); err != nil {
			return err
		}
		resumeSummary = fmt.Sprintf("Session %s resumed (%d messages). Exit: save + resume with: tau --resume %s",
			sessionID, len(loaded.Messages), sessionID)
	} else {
		config := buildSessionConfig(opts, model, systemPrompt)
		if err := coordinator.Send(tauchat.StartChatSessionCommand{SessionID: sessionID, Config: config}); err != nil {
			return err
		}
	}

	// Build a refresher closure that the TUI can use to re-discover models.
	refresher := buildModelRefresher(pr)

	// Command registry owns command state; TUI initialises from snapshot
	// and receives deltas via CommandsChangedEvent on the bus.
	initialCommands := commandRefsFromRegistry(result.CommandRegistry.All())

	// Extract the initial active skill set to seed the Web UI bridge.
	initialSkills := skillsFromManager(skillsMgr)

	// Optionally start the Web UI bridge and server.
	var webURL string
	var webShutdown func()
	var webWait func()
	if !opts.NoWeb {
		webRes, err := startWebUI(coordinator, bus, opts, sessionID, model.ID, available, tauconfig.ProviderNames(opts.Config), initialCommands, initialSkills, result.ExtensionCommands, slog.Default())
		if err != nil {
			slog.Warn("web ui failed", "err", err)
		} else if webRes != nil {
			webURL = webRes.URL
			webShutdown = webRes.Shutdown
			webWait = webRes.Wait
			if opts.Web {
				if err := openBrowser(ctx, webURL); err != nil {
					slog.Warn("open browser failed", "err", err)
				}
			}
		}
	}

	tuiCfg := tui.TUIConfig{
		SessionID:          sessionID,
		ModelName:          model.ID,
		Provider:           opts.Provider.Name,
		AvailableModels:    available,
		AvailableProviders: tauconfig.ProviderNames(opts.Config),
		InitialCommands:    initialCommands,
		Bus:                bus,
		RefreshModels:      refresher,
		ShowReasoning:      opts.Config.UI.ShowReasoning,
		ReasoningEffort:    opts.ReasoningEffort,
		Debug:              isDevel(opts.Version, opts.Config),
		WebURL:             webURL,
	}

	tuiErr := tui.Run(ctx, coordinator, tuiCfg)

	// Shut down the web UI before closing the coordinator.
	if webShutdown != nil {
		webShutdown()
		if webWait != nil {
			webWait()
		}
	}

	// Close coordinator first so it can persist sessions while the store is still open.
	coordinator.Close()

	// Print session summary on exit.
	if sessionManager != nil {
		printExitSummary(ctx, sessionManager, sessionID, resumeSummary)
		if err := sessionManager.Close(); err != nil {
			slog.Warn("closing session store", "err", err)
		}
	}

	return tuiErr
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

// skillsFromManager extracts a lightweight SkillInfo slice from the skills
// manager's active skill snapshot, suitable for seeding the Web UI bridge.
func skillsFromManager(mgr *skills.Manager) []tauchat.SkillInfo {
	snapshot := mgr.Snapshot()
	out := make([]tauchat.SkillInfo, 0, len(snapshot.ActiveSkills))
	for _, s := range snapshot.ActiveSkills {
		out = append(out, tauchat.SkillInfo{
			Name:        s.Name,
			Description: s.Description,
			Scope:       string(s.Scope),
		})
	}
	return out
}
