package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/provider"
	"github.com/samcharles93/tau/internal/pubsub"
	"github.com/samcharles93/tau/internal/store"
	"github.com/samcharles93/tau/internal/tui"
	"github.com/samcharles93/tau/internal/tui/notify"
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
	sessionsDir := tauconfig.SessionsDir()
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		slog.Warn("session store: could not create sessions dir", "err", err)
	}
	storePath := tauconfig.SessionsDBPath()
	sessionStore, storeErr := store.NewSQLiteStore(storePath, sessionsDir)
	if storeErr != nil {
		slog.Warn("session store unavailable, sessions will not be persisted", "err", storeErr)
		sessionStore = nil
	}

	coordinator, err := newCoordinator(ctx, opts, bearerToken, sessionStore)
	if err != nil {
		if sessionStore != nil {
			sessionStore.Close()
		}
		return err
	}
	defer coordinator.Close()

	sessionID, err := newID()
	if err != nil {
		return err
	}

	// If --resume is set, load the session and use its ID/properties.
	resumeSummary := "" // printed on exit
	if opts.ResumeSessionID != "" && sessionStore != nil {
		resumeID := opts.ResumeSessionID
		if resumeID == "latest" {
			summaries, _, lErr := sessionStore.List(ctx, 1, "")
			if lErr != nil || len(summaries) == 0 {
				return fmt.Errorf("no saved sessions to resume")
			}
			resumeID = summaries[0].ID
		}
		loaded, lErr := sessionStore.Load(ctx, resumeID)
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

	notifyBus := pubsub.New[notify.Notification]()
	defer notifyBus.Close()

	available := buildModelRefs(allModels)

	// Build a refresher closure that captures the provider and token so the
	// TUI can re-discover models without importing infrastructure packages.
	refresher := buildModelRefresher(opts.Provider, bearerToken, opts.Insecure)

	tuiCfg := tui.TUIConfig{
		SessionID:          sessionID,
		ModelName:          model.ID,
		Provider:           opts.Provider.Name,
		AvailableModels:    available,
		AvailableProviders: tauconfig.ProviderNames(opts.Config),
		NotifyBus:          notifyBus,
		RefreshModels:      refresher,
		ShowReasoning:      opts.Config.UI.ShowReasoning,
		Debug:              isDevel(opts.Version, opts.Config),
	}

	// If model discovery failed at startup, notify the user in the TUI
	// rather than refusing to start.
	if discoverErr != nil {
		_ = notifyBus.Publish(ctx, "notifications", notify.Notification{
			Message:  "Model discovery failed: " + discoverErr.Error() + " (`/refresh` to retry)",
			Level:    notify.LevelWarn,
			Duration: 8 * time.Second,
		})
	}

	tuiErr := tui.Run(ctx, coordinator, tuiCfg)

	// Close coordinator first so it can persist sessions while the store is still open.
	coordinator.Close()

	// Print session summary on exit.
	if sessionStore != nil {
		printExitSummary(ctx, sessionStore, sessionID, resumeSummary)
		sessionStore.Close()
	}

	return tuiErr
}
