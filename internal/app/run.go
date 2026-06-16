package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/provider"
	commandreg "github.com/samcharles93/tau/internal/registry"
	"github.com/samcharles93/tau/internal/sessions"
	"github.com/samcharles93/tau/internal/tui"
)

// RunChat orchestrates an interactive chat session: resolves tokens and model,
// creates the coordinator, then launches the TUI.
func RunChat(ctx context.Context, opts ChatOptions) error {
	bearerToken, err := provider.ResolveBearerToken(ctx, opts.Provider, opts.Insecure)
	if err != nil {
		return err
	}

	allModels, discoverErr := provider.DiscoverModels(ctx, opts.Provider, bearerToken, opts.Insecure)
	model, err := pickModel(allModels, opts.Model, opts.Config.DefaultModel, opts.Provider.BaseURL)
	if err != nil {
		return err
	}

	// Build the full system prompt — project context (AGENTS.md) + user override.
	cwd, _ := os.Getwd()
	systemPrompt := buildAgentSystemPrompt("", cwd) // Left user prompt there in case we want to expand this later.

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

	result, err := newCoordinator(ctx, opts, bearerToken, sessionManager, startupEvents, bus)
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
			loadedModel, pickErr := pickModel(allModels, loaded.Model.ID, "", opts.Provider.BaseURL)
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

	available := buildModelRefs(allModels)

	// Build a refresher closure that captures the provider and token so the
	// TUI can re-discover models without importing infrastructure packages.
	refresher := buildModelRefresher(opts.Provider, bearerToken, opts.Insecure)

	// Command registry owns command state; TUI initialises from snapshot
	// and receives deltas via CommandsChangedEvent on the bus.
	initialCommands := commandRefsFromRegistry(result.CommandRegistry.All())

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
		Debug:              isDevel(opts.Version, opts.Config),
	}

	tuiErr := tui.Run(ctx, coordinator, tuiCfg)

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
